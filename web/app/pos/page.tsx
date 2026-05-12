"use client"

import { useReducer, useRef, useState, useEffect, useCallback, lazy, Suspense } from "react"
import Decimal from "decimal.js"
import {
  cartReducer,
  cartNetTotal,
  initialCartState,
  type CartItem,
} from "@/lib/pos/cart-reducer"
import { usePosHotkeys } from "@/lib/pos/hotkeys"
import { quickCheckout, type PaymentMethod } from "@/lib/api/pos"
import type { Product } from "@/lib/api/products"
import { ProductSearch } from "@/components/pos/product-search"
import { Cart } from "@/components/pos/cart"
import { PaymentModal, type PaymentMode } from "@/components/pos/payment-modal"
import { CheckoutSuccess } from "@/components/pos/checkout-success"
import { useConfirm } from "@/hooks/useConfirm"
import { ErrorBanner } from "@/components/ui/error-banner"

const ProductGrid = lazy(() =>
  import("@/components/pos/product-grid").then((m) => ({ default: m.ProductGrid }))
)

// Story 2.1 TODO: replace with session tenantId once auth wired
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID
// MVP: use a configured default warehouse, or prompt user if absent
const defaultWarehouseId = process.env.NEXT_PUBLIC_DEFAULT_WAREHOUSE_ID ?? ""

interface CheckoutResult {
  billNo: string
  totalAmount: string
}

/**
 * POS main page — the retail cashier interface.
 *
 * Left 60%: product search + product grid
 * Right 40%: shopping cart + checkout buttons
 *
 * Keyboard shortcuts: F1 (search), F2 (qty), F3 (pay), F4 (cancel)
 */
export default function PosPage() {
  const [cartState, dispatch] = useReducer(cartReducer, initialCartState)
  const [paymentMode, setPaymentMode] = useState<PaymentMode | null>(null)
  const [checkoutResult, setCheckoutResult] = useState<CheckoutResult | null>(null)
  const [checkoutError, setCheckoutError] = useState<string | null>(null)
  const [checkoutLoading, setCheckoutLoading] = useState(false)
  const [allProducts, setAllProducts] = useState<Product[]>([])

  const searchRef = useRef<HTMLInputElement>(null)
  const lastQtyRef = useRef<HTMLInputElement>(null)

  // Load product list for the grid on mount
  useEffect(() => {
    import("@/lib/api/products").then(({ listProducts }) => {
      listProducts({ tenantId: devTenantId, limit: 100 })
        .then((res) => setAllProducts(res.items ?? []))
        .catch(() => {
          // Non-fatal: grid may be empty, user can still search
        })
    })
  }, [])

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
    const item: CartItem = {
      productId: product.id,
      productName: product.name,
      unitId: product.default_unit_id ?? "default",
      unitName: "pcs",
      unitPrice: new Decimal("0.00"), // TODO: load from product price list
      quantity: new Decimal(1),
      measurementStrategy: product.measurement_strategy,
    }
    dispatch({ type: "ADD_ITEM", item })
  }, [])

  const handlePaymentConfirm = useCallback(
    async (args: { paymentMethod: PaymentMethod; paidAmount: Decimal; customerName?: string }) => {
      if (!defaultWarehouseId) {
        setCheckoutError("请先配置默认仓库 (NEXT_PUBLIC_DEFAULT_WAREHOUSE_ID)")
        return
      }

      setCheckoutLoading(true)
      setCheckoutError(null)

      try {
        const total = cartNetTotal(cartState)
        const result = await quickCheckout(
          {
            items: cartState.items.map((item) => ({
              product_id: item.productId,
              warehouse_id: defaultWarehouseId,
              qty: item.quantity.toString(),
              unit_id: item.unitId,
              unit_price: item.unitPrice.toFixed(4),
            })),
            payment_method: args.paymentMethod,
            paid_amount: args.paidAmount.toFixed(2),
            customer_name: args.customerName,
          },
          devTenantId
        )

        setPaymentMode(null)
        setCheckoutResult({ billNo: result.bill_no, totalAmount: total.toFixed(2) })
      } catch (err) {
        const msg = String(err)
        if (msg.includes("insufficient_stock")) {
          setCheckoutError("库存不足，请检查商品库存后重试")
        } else {
          setCheckoutError(`结账失败：${msg}`)
        }
      } finally {
        setCheckoutLoading(false)
      }
    },
    [cartState]
  )

  const handleSuccessDismiss = useCallback(() => {
    setCheckoutResult(null)
    dispatch({ type: "CLEAR_CART" })
    searchRef.current?.focus()
  }, [])

  return (
    <div className="flex min-h-screen flex-col pt-8 md:h-screen md:overflow-hidden">
      {/* Header */}
      <div className="shrink-0 border-b border-border bg-background px-4 py-2">
        <h1 className="text-base font-semibold">POS 收银台</h1>
      </div>

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

      {/* No warehouse warning */}
      {!defaultWarehouseId && (
        <div className="shrink-0 bg-warning/10 px-4 py-1.5 text-xs text-warning border-b border-warning/20">
          未配置默认仓库 (NEXT_PUBLIC_DEFAULT_WAREHOUSE_ID)，结账按钮将被禁用
        </div>
      )}

      {/* Main two-column layout — stacks on mobile, splits 60/40 on md+ */}
      <div className="flex flex-1 flex-col md:flex-row md:overflow-hidden">
        {/* Left: product discovery */}
        <div className="flex w-full flex-col gap-3 border-b border-border p-4 md:w-3/5 md:overflow-hidden md:border-b-0 md:border-r">
          <ProductSearch
            ref={searchRef}
            onSelect={addToCart}
            tenantId={devTenantId}
          />
          <Suspense fallback={<div className="text-sm text-muted-foreground">加载商品...</div>}>
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
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/30">
          <div className="rounded-xl bg-background p-6 text-center shadow-xl">
            <div className="text-lg font-medium">处理中...</div>
          </div>
        </div>
      )}

      {/* Success overlay */}
      {checkoutResult && (
        <CheckoutSuccess
          billNo={checkoutResult.billNo}
          totalAmount={checkoutResult.totalAmount}
          onDismiss={handleSuccessDismiss}
        />
      )}
    </div>
  )
}
