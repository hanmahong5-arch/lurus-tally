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

  const handleCancelRequested = useCallback(() => {
    if (window.confirm("确认清空购物车？")) {
      dispatch({ type: "CLEAR_CART" })
    }
  }, [])

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
    <div className="flex h-screen flex-col overflow-hidden pt-8">
      {/* Header */}
      <div className="shrink-0 border-b border-border bg-background px-4 py-2">
        <h1 className="text-base font-semibold">POS 收银台</h1>
      </div>

      {/* Checkout error banner */}
      {checkoutError && (
        <div className="shrink-0 bg-destructive/10 px-4 py-2 text-sm text-destructive border-b border-destructive/20">
          {checkoutError}
          <button
            className="ml-2 underline"
            onClick={() => setCheckoutError(null)}
          >
            关闭
          </button>
        </div>
      )}

      {/* No warehouse warning */}
      {!defaultWarehouseId && (
        <div className="shrink-0 bg-warning/10 px-4 py-1.5 text-xs text-warning border-b border-warning/20">
          未配置默认仓库 (NEXT_PUBLIC_DEFAULT_WAREHOUSE_ID)，结账按钮将被禁用
        </div>
      )}

      {/* Main two-column layout */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left: 60% — product discovery */}
        <div className="flex w-3/5 flex-col gap-3 overflow-hidden border-r border-border p-4">
          <ProductSearch
            ref={searchRef}
            onSelect={addToCart}
            tenantId={devTenantId}
          />
          <Suspense fallback={<div className="text-sm text-muted-foreground">加载商品...</div>}>
            <ProductGrid products={allProducts} onAdd={addToCart} />
          </Suspense>
        </div>

        {/* Right: 40% — cart + checkout */}
        <div className="flex w-2/5 flex-col overflow-hidden p-4">
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
