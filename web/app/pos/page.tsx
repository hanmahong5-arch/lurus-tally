"use client"

import Link from "next/link"
import { useReducer, useRef, useState, useEffect, useCallback, lazy, Suspense } from "react"
import Decimal from "decimal.js"
import {
  cartReducer,
  cartNetTotal,
  cartTotal,
  initialCartState,
  type CartItem,
} from "@/lib/pos/cart-reducer"
import { usePosHotkeys } from "@/lib/pos/hotkeys"
import { quickCheckout, type PaymentMethod, type QuickCheckoutRequest } from "@/lib/api/pos"
import type { Product } from "@/lib/api/products"
import { ProductSearch } from "@/components/pos/product-search"
import { Cart } from "@/components/pos/cart"
import { PaymentModal, type PaymentMode } from "@/components/pos/payment-modal"
import { CheckoutSuccess } from "@/components/pos/checkout-success"
import { PosReturn } from "@/components/pos/pos-return"
import { useConfirm } from "@/hooks/useConfirm"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { ErrorBanner } from "@/components/ui/error-banner"
import { ApiError, NetworkError, extractApiError } from "@/lib/api/errors"
import { SIGNOUT_FAILED_EVENT } from "@/lib/device-keys"
import { useSubmitKey } from "@/lib/submit-key"
import { saveCart, loadCart, clearCart } from "@/components/pos/cart-storage"
import { PendingCheckouts } from "@/components/pos/pending-checkouts"
import { useCheckoutQueue } from "@/hooks/use-checkout-queue"
import { newClientRef, offlineRefOf, shouldEnqueue } from "@/lib/pos/checkout-queue"
import { refreshCatalog } from "@/lib/catalog-cache"
import { prefetchCashierCapabilities } from "@/lib/pos/capability-prefetch"
import type { QueueEntry } from "@/lib/pos/checkout-queue"

/** 收银目录缓存的后台刷新间隔。 */
const CATALOG_REFRESH_MS = 10 * 60_000

const ProductGrid = lazy(() =>
  import("@/components/pos/product-grid").then((m) => ({ default: m.ProductGrid }))
)

function retailPriceOf(product: Product): Decimal {
  const raw = product.retail_price
  if (raw === undefined || raw === null || raw === "") return new Decimal(0)
  try {
    const d = new Decimal(raw)
    return d.isFinite() && !d.isNegative() ? d : new Decimal(0)
  } catch {
    return new Decimal(0)
  }
}

/**
 * True unless the SKU price is quoted in a unit other than the product's default unit.
 * 单位表里查不到该单位（离线打开、加载失败）时无从比对，信任商品自带的报价单位（目录缓存里的价）。
 */
function priceUnitMatches(product: Product, unitNames: Record<string, string>): boolean {
  const priceUnit = product.retail_price_unit
  if (!priceUnit || !product.default_unit_id) return true
  const name = unitNames[product.default_unit_id]
  if (name === undefined) return true
  return name === priceUnit
}

interface CheckoutResult {
  /** 离线入队的单没有服务器单据 id。 */
  billId?: string
  /** 离线入队的单号（OFF-xxxx-n）；有值时界面显示「离线单号」。 */
  offlineRef?: string
  /** 离线入队的台账条目，离线小票据此渲染。 */
  offlineEntry?: QueueEntry
  billNo: string
  totalAmount: string
  /** Cash handed over and change given, for the receipt; only set for a cash sale. */
  receivedAmount?: string
  changeAmount?: string
  owedAmount?: string
}

interface InsufficientStockInfo {
  product_id: string
  available: string
  requested: string
}

/** quick-checkout returns {error, details:[{product_id, available_qty, requested_qty}]}. */
function parseInsufficientStock(b: unknown): InsufficientStockInfo | null {
  if (!b || typeof b !== "object") return null
  const o = b as Record<string, unknown>
  if (o.error !== "insufficient_stock" || !Array.isArray(o.details)) return null
  const d = o.details[0] as Record<string, unknown> | undefined
  if (!d || typeof d.product_id !== "string") return null
  return {
    product_id: d.product_id,
    available: String(d.available_qty ?? "?"),
    requested: String(d.requested_qty ?? "?"),
  }
}

/** 409 from the idempotency guard: the same-key request has not finished yet. */
function isInFlight(err: unknown): boolean {
  if (!(err instanceof ApiError) || err.status !== 409) return false
  const b = err.body as { error?: unknown } | null | undefined
  return err.code === "idempotency_in_flight" || b?.error === "idempotency_in_flight"
}

/** 422 bodies from quick-checkout carry a Chinese `message`; fall back to a generic hint. */
function checkoutMessage(err: ApiError): string {
  const b = err.body as { message?: unknown } | null | undefined
  return typeof b?.message === "string" && b.message ? b.message : "结账失败，请检查单据后重试"
}

/**
 * POS main page. Roles without checkout (clerk, finance) get a pointer instead
 * of the till; the API refuses them anyway. An unknown role still shows the till.
 */
export default function PosPage() {
  const { known, can } = useRole()
  if (known && !can("bill.checkout")) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-3 px-4 text-center">
        <p className="text-base text-foreground">收银请找收银员或老板</p>
        <Link href="/dashboard" className="text-sm text-primary hover:underline">
          回首页
        </Link>
      </div>
    )
  }
  return <PosTill />
}

/**
 * The retail cashier interface.
 *
 * Left 60%: product search + product grid
 * Right 40%: shopping cart + checkout buttons
 *
 * Keyboard shortcuts: F1 (search), F2 (qty), F3 (pay), F4 (cancel)
 */
function PosTill() {
  const [cartState, dispatch] = useReducer(cartReducer, initialCartState)
  const [paymentMode, setPaymentMode] = useState<PaymentMode | null>(null)
  const [checkoutResult, setCheckoutResult] = useState<CheckoutResult | null>(null)
  const [checkoutError, setCheckoutError] = useState<string | null>(null)
  const [checkoutLoading, setCheckoutLoading] = useState(false)
  // Customer return at the till: find the receipt, pick the goods, optionally refund.
  const [returnOpen, setReturnOpen] = useState(false)
  // Returns: the boss and manager take any, a cashier only today's own; others never see the entry.
  const { can } = useRole()
  const mayReturn = can("bill.return") || can("bill.return.own")
  const [returnNotice, setReturnNotice] = useState<string | null>(null)
  const [allProducts, setAllProducts] = useState<Product[]>([])
  const [unitNames, setUnitNames] = useState<Record<string, string>>({})
  // Until the unit list answers, a line's unit label and price-unit check are unknown.
  const [unitsLoaded, setUnitsLoaded] = useState(false)
  // The cart is only mirrored to sessionStorage after the saved one has been read back.
  const [cartHydrated, setCartHydrated] = useState(false)
  // One Idempotency-Key per checkout attempt: reused only when the reply was lost.
  const checkoutKey = useSubmitKey()
  // Fingerprint of the last submit whose reply never came back: it may have gone through.
  const lostRef = useRef<string | null>(null)
  // 结账本地台账：断网（请求从未发出）时入队，联网后自动上传。
  const queue = useCheckoutQueue()
  // 同一笔单（金额不变）重试时沿用同一个 client_ref 与发生时间，服务器据此去重。
  const attemptRef = useRef<{ fingerprint: string; clientRef: string; occurredAt: number } | null>(null)

  const searchRef = useRef<HTMLInputElement>(null)
  const lastQtyRef = useRef<HTMLInputElement>(null)

  const tenantId = useTenantId()
  // Dev strict mode runs effects twice; restoring twice would double every quantity.
  const restoredRef = useRef(false)

  // Restore a cart that survived a sign-in redirect or refresh.
  useEffect(() => {
    if (restoredRef.current) return
    restoredRef.current = true
    const saved = loadCart()
    if (saved) {
      for (const item of saved.items) dispatch({ type: "ADD_ITEM", item })
      if (!saved.discount.isZero()) {
        dispatch({ type: "APPLY_DISCOUNT", amount: saved.discount, discountType: saved.discountType })
      }
      if (saved.remark) dispatch({ type: "SET_REMARK", remark: saved.remark })
    }
    setCartHydrated(true)
  }, [])

  useEffect(() => {
    if (cartHydrated) saveCart(cartState)
  }, [cartState, cartHydrated])

  // A sign-out that failed midway blocked saves for a while; save what is in memory now.
  const cartStateRef = useRef(cartState)
  cartStateRef.current = cartState
  useEffect(() => {
    const onFailed = () => saveCart(cartStateRef.current)
    window.addEventListener(SIGNOUT_FAILED_EVENT, onFailed)
    return () => window.removeEventListener(SIGNOUT_FAILED_EVENT, onFailed)
  }, [])

  // 后台刷新收银目录缓存（断网时靠它搜商品）：挂载一次，之后每 10 分钟一次。
  useEffect(() => {
    if (!tenantId) return
    void refreshCatalog(tenantId)
    const timer = setInterval(() => void refreshCatalog(tenantId), CATALOG_REFRESH_MS)
    return () => clearInterval(timer)
  }, [tenantId])

  // 预取云打印能力与打印设置到本机缓存：新设备第一单就知道该不该走云打印。
  useEffect(() => {
    const ctrl = new AbortController()
    void prefetchCashierCapabilities(tenantId, ctrl.signal)
    return () => ctrl.abort()
  }, [tenantId])

  // Load product list for the grid on mount (re-runs once the session tenant resolves)
  useEffect(() => {
    import("@/lib/api/products").then(({ listProducts }) => {
      listProducts({ tenantId, limit: 200 })
        .then((res) => setAllProducts(res.items ?? []))
        .catch(() => {
          // Non-fatal: grid may be empty, user can still search
        })
    })
  }, [tenantId])

  // Unit id -> display name so the default unit shows on the cart line and receipt.
  useEffect(() => {
    import("@/lib/api/units").then(({ listUnits }) => {
      listUnits(undefined, tenantId)
        .then((res) =>
          setUnitNames(Object.fromEntries((res.items ?? []).map((u) => [u.id, u.name]))),
        )
        .catch(() => {
          // Non-fatal: lines just show no unit label.
        })
        .finally(() => setUnitsLoaded(true))
    })
  }, [tenantId])

  const openPaymentModal = useCallback((method: PaymentMethod) => {
    if (cartState.items.length === 0) return
    setPaymentMode(method as PaymentMode)
    setCheckoutError(null)
  }, [cartState.items.length])

  const confirm = useConfirm()
  const handleCancelRequested = useCallback(async () => {
    const ok = await confirm({
      title: "清空购物车",
      body: "当前购物车中的商品将全部移除，操作不可撤销。",
      confirmText: "清空",
      cancelText: "保留",
      danger: true,
    })
    if (ok) dispatch({ type: "CLEAR_CART" })
  }, [confirm])

  usePosHotkeys({
    searchRef: searchRef as React.RefObject<HTMLInputElement | null>,
    lastQtyRef: lastQtyRef as React.RefObject<HTMLInputElement | null>,
    onPayRequested: () => openPaymentModal("cash"),
    onCancelRequested: handleCancelRequested,
  })

  const addToCart = useCallback((product: Product) => {
    // A product with a default unit needs the unit list to label the line and to
    // trust its price; adding earlier would bake in a blank unit and a zero price.
    if (product.default_unit_id && !unitsLoaded) return
    const item: CartItem = {
      productId: product.id,
      productName: product.name,
      unitId: product.default_unit_id ?? "",
      // 单位表没有该单位时（离线），回填目录缓存里的报价单位名。
      unitName:
        (product.default_unit_id && unitNames[product.default_unit_id]) ||
        (product.default_unit_id ? product.retail_price_unit ?? "" : ""),
      // 0 = no catalogue price: the cart flags the line and the cashier must type one.
      // A price quoted in a different unit than the line's unit is unsafe to reuse: also 0.
      unitPrice: priceUnitMatches(product, unitNames) ? retailPriceOf(product) : new Decimal(0),
      quantity: new Decimal(1),
      measurementStrategy: product.measurement_strategy,
    }
    dispatch({ type: "ADD_ITEM", item })
  }, [unitNames, unitsLoaded])

  const handlePaymentConfirm = useCallback(
    async (args: {
      paymentMethod: PaymentMethod
      paidAmount: Decimal
      partnerId?: string
      customerName?: string
      note?: string
    }) => {
      setCheckoutLoading(true)
      setCheckoutError(null)
      let lastFingerprint: string | null = null
      // 本次请求的 body 与发生时间，断网入队时原样带走。
      let sent: { body: QuickCheckoutRequest; clientRef: string; occurredAt: number; total: string } | null = null

      try {
        // The discount is sent as an explicit amount so the bill total matches the
        // payable shown to the cashier (net rounded to cents).
        const subtotal = cartTotal(cartState.items)
        const net = cartNetTotal(cartState).toDecimalPlaces(2)
        const discount = Decimal.max(subtotal.minus(net), new Decimal(0))
        const coreBody: QuickCheckoutRequest = {
          items: cartState.items.map((item) => ({
            product_id: item.productId,
            // warehouse_id omitted: the backend falls back to the tenant's default warehouse.
            qty: item.quantity.toString(),
            unit_id: item.unitId || undefined,
            // Resolved now, not at add time: the unit list may have loaded after the tap.
            unit_name: (item.unitId && unitNames[item.unitId]) || item.unitName || undefined,
            unit_price: item.unitPrice.toFixed(4),
          })),
          payment_method: args.paymentMethod,
          paid_amount: args.paidAmount.toFixed(2),
          // A credit sale is booked to the picked customer; other sales keep a free note
          // (e.g. the receipt's last digits) as the bill remark.
          partner_id: args.partnerId,
          customer_name: args.partnerId ? undefined : args.note ?? args.customerName,
          remark: cartState.remark.trim() || undefined,
          discount_amount: discount.isZero() ? undefined : discount.toFixed(4),
        }
        let body: QuickCheckoutRequest = coreBody
        if (queue.available) {
          const coreFp = JSON.stringify(coreBody)
          let at = attemptRef.current
          if (!at || at.fingerprint !== coreFp) {
            at = { fingerprint: coreFp, clientRef: newClientRef(), occurredAt: Date.now() }
            attemptRef.current = at
          }
          body = { ...coreBody, client_ref: at.clientRef, occurred_at: new Date(at.occurredAt).toISOString() }
          sent = { body, clientRef: at.clientRef, occurredAt: at.occurredAt, total: net.toFixed(2) }
        }
        // The reply to the last tap was lost and the amounts changed: a new key would ring a
        // second bill if the first one landed, so make the cashier look at today's records first.
        const fingerprint = JSON.stringify(body)
        lastFingerprint = fingerprint
        if (lostRef.current !== null && lostRef.current !== fingerprint) {
          const ok = await confirm({
            title: "上一笔可能已经结账成功",
            body: "刚才那一笔没有收到回复，可能已经结账成功。请先到「今日结单」看一下，确认没有这一笔，再继续结账。",
            confirmText: "已核对，继续结账",
            cancelText: "先去看看",
          })
          if (!ok) return
          lostRef.current = null
        }
        const result = await quickCheckout(body, tenantId, checkoutKey.keyFor(body))
        checkoutKey.succeeded()
        lostRef.current = null
        attemptRef.current = null
        // Sold: a reload before the overlay closes must not bring the cart back.
        clearCart()

        setPaymentMode(null)
        setCheckoutResult({
          billId: result.bill_id,
          billNo: result.bill_no,
          totalAmount: new Decimal(result.total_amount).toFixed(2),
          receivedAmount: args.paymentMethod === "cash" ? args.paidAmount.toFixed(2) : undefined,
          changeAmount: args.paymentMethod === "cash" ? new Decimal(result.change_amount || "0").toFixed(2) : undefined,
          owedAmount: result.receivable_amount,
        })
      } catch (err) {
        if (isInFlight(err)) {
          // The earlier attempt is still being booked: keep its key so the next tap
          // replays it instead of ringing up a second bill.
          setCheckoutError("上一笔还在处理，请稍候再点确认")
          lostRef.current = lostRef.current ?? lastFingerprint
          return
        }
        // 请求从未发出（断网）：存进本地台账，购物车清空，联网后自动上传。服务器回的任何错误都不入队。
        if (sent && shouldEnqueue(err)) {
          const entry = await queue.enqueue({
            clientRef: sent.clientRef,
            idempotencyKey: crypto.randomUUID(),
            body: { ...sent.body, source: "pos_offline" },
            occurredAt: sent.occurredAt,
            totalAmount: sent.total,
            itemNames: Object.fromEntries(cartState.items.map((i) => [i.productId, i.productName])),
          })
          if (entry) {
            checkoutKey.succeeded()
            lostRef.current = null
            attemptRef.current = null
            clearCart()
            setPaymentMode(null)
            setCheckoutResult({
              offlineRef: offlineRefOf(entry.client_ref),
              offlineEntry: entry,
              billNo: "",
              totalAmount: sent.total,
              receivedAmount: args.paymentMethod === "cash" ? args.paidAmount.toFixed(2) : undefined,
              changeAmount: args.paymentMethod === "cash" ? Decimal.max(args.paidAmount.minus(sent.total), 0).toFixed(2) : undefined,
            })
            return
          }
        }
        checkoutKey.failed(err)
        const replyLost =
          err instanceof NetworkError ||
          (err instanceof ApiError && (err.status === 502 || err.status === 503 || err.status === 504))
        lostRef.current = replyLost ? lastFingerprint : null
        if (err instanceof ApiError && err.status === 422 && parseInsufficientStock(err.body)) {
          const body = parseInsufficientStock(err.body)!
          const product = allProducts.find((p) => p.id === body.product_id)
          const name = product?.name ?? "该商品"
          setCheckoutError(
            `「${name}」库存不足：可用 ${body.available}，本单需要 ${body.requested}`,
          )
        } else if (err instanceof ApiError && err.status === 422) {
          setCheckoutError(checkoutMessage(err))
        } else {
          setCheckoutError(`结账失败：${extractApiError(err).message}`)
        }
      } finally {
        setCheckoutLoading(false)
      }
    },
    [cartState, allProducts, tenantId, unitNames, checkoutKey, confirm, queue]
  )

  const handleSuccessDismiss = useCallback(() => {
    setCheckoutResult(null)
    dispatch({ type: "CLEAR_CART" })
    searchRef.current?.focus()
  }, [])

  return (
    <div className="flex min-h-screen flex-col pt-8 md:h-screen md:overflow-hidden">
      {/* Header — title left, live cart summary right (POS-grade at-a-glance total) */}
      <div className="flex shrink-0 items-center justify-between gap-4 border-b border-border bg-background px-4 py-2">
        <div className="flex items-center gap-2">
          <span aria-hidden="true" className="text-base">🖥️</span>
          <h1 className="text-base font-semibold">POS 收银台</h1>
        </div>
        <PendingCheckouts entries={queue.entries} onRetry={queue.retry} onDiscard={queue.discard} />
        {mayReturn && (
          <button
            type="button"
            onClick={() => setReturnOpen(true)}
            className="inline-flex min-h-[36px] items-center rounded-md border border-border px-3 py-1.5 text-xs text-foreground transition-colors hover:bg-muted"
          >
            客户退货
          </button>
        )}
        <div className="flex items-baseline gap-2 tabular-nums" aria-live="polite">
          <span className="text-xs text-muted-foreground">
            {cartState.items.length} 件
          </span>
          <span className="font-mono text-lg font-semibold text-foreground">
            ¥{cartNetTotal(cartState).toFixed(2)}
          </span>
        </div>
      </div>

      {returnNotice && (
        <div
          role="status"
          className="shrink-0 border-b border-border bg-card px-4 py-2 text-sm text-success"
        >
          {returnNotice}
          <button
            type="button"
            className="ml-3 text-xs text-muted-foreground underline"
            onClick={() => setReturnNotice(null)}
          >
            知道了
          </button>
        </div>
      )}

      {/* Checkout error banner */}
      {checkoutError && (
        <div className="shrink-0">
          <ErrorBanner
            className="rounded-none border-x-0 border-t-0"
            onDismiss={() => setCheckoutError(null)}
          >
            {checkoutError}
          </ErrorBanner>
        </div>
      )}

      {/* Main two-column layout — stacks on mobile, splits 60/40 on md+ */}
      <div className="flex flex-1 flex-col md:flex-row md:overflow-hidden">
        {/* Left: product discovery */}
        <div className="flex w-full flex-col gap-3 border-b border-border p-4 md:w-3/5 md:overflow-hidden md:border-b-0 md:border-r">
          {!unitsLoaded && (
            <p className="text-xs text-muted-foreground" role="status">
              正在加载单位信息，请稍候再点商品…
            </p>
          )}
          <ProductSearch
            ref={searchRef}
            onSelect={addToCart}
            tenantId={tenantId}
          />
          <Suspense
            fallback={
              <div
                className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4"
                aria-label="加载商品中"
              >
                {Array.from({ length: 8 }).map((_, i) => (
                  <div
                    key={i}
                    className="h-24 animate-pulse rounded-xl border border-border bg-muted/40"
                  />
                ))}
              </div>
            }
          >
            <ProductGrid products={allProducts} onAdd={addToCart} />
          </Suspense>
        </div>

        {/* Right: cart + checkout */}
        <div className="flex w-full flex-col p-4 md:w-2/5 md:overflow-hidden">
          <Cart
            state={cartState}
            dispatch={dispatch}
            onCheckout={openPaymentModal}
          />
        </div>
      </div>

      {/* 手机端粘底结账条：合计 + 结账；桌面用右侧购物车里的结账按钮 */}
      <div aria-hidden="true" className="h-20 shrink-0 md:hidden" />
      <div
        className="fixed inset-x-0 bottom-0 z-30 flex items-center justify-between gap-3 border-t border-border bg-background px-4 pt-2 md:hidden"
        style={{ paddingBottom: "calc(0.5rem + env(safe-area-inset-bottom))" }}
        data-testid="pos-mobile-bar"
      >
        <div className="flex items-baseline gap-2 tabular-nums">
          <span className="text-xs text-muted-foreground">合计</span>
          <span className="font-mono text-xl font-semibold text-foreground">
            ¥{cartNetTotal(cartState).toFixed(2)}
          </span>
        </div>
        <button
          type="button"
          disabled={cartState.items.length === 0 || checkoutLoading}
          onClick={() => openPaymentModal("cash")}
          className="inline-flex min-h-[44px] items-center rounded-md bg-primary px-6 text-base font-semibold text-primary-foreground disabled:opacity-50"
        >
          结账
        </button>
      </div>

      <PosReturn
        open={returnOpen}
        onOpenChange={setReturnOpen}
        tenantId={tenantId}
        onDone={setReturnNotice}
      />

      {/* Payment modal */}
      {paymentMode && (
        <PaymentModal
          open={true}
          mode={paymentMode}
          totalAmount={cartNetTotal(cartState)}
          loading={checkoutLoading}
          onConfirm={handlePaymentConfirm}
          onClose={() => setPaymentMode(null)}
        />
      )}

      {/* Loading overlay during checkout API call */}
      {checkoutLoading && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/30 backdrop-blur-sm" role="status" aria-live="polite">
          <div className="flex flex-col items-center gap-3 rounded-2xl bg-background px-8 py-6 shadow-xl">
            <span
              className="h-8 w-8 animate-spin rounded-full border-[3px] border-muted border-t-primary"
              aria-hidden="true"
            />
            <div className="text-sm font-medium">正在结账并出库…</div>
          </div>
        </div>
      )}

      {/* Success overlay */}
      {checkoutResult && (
        <CheckoutSuccess
          billNo={checkoutResult.billNo}
          billId={checkoutResult.billId}
          offlineRef={checkoutResult.offlineRef}
          offlineEntry={checkoutResult.offlineEntry}
          totalAmount={checkoutResult.totalAmount}
          receivedAmount={checkoutResult.receivedAmount}
          changeAmount={checkoutResult.changeAmount}
          owedAmount={checkoutResult.owedAmount}
          onDismiss={handleSuccessDismiss}
        />
      )}
    </div>
  )
}
