"use client"

import React from "react"
import Decimal from "decimal.js"
import {
  cartTotal,
  cartNetTotal,
  type CartState,
  type CartAction,
} from "@/lib/pos/cart-reducer"
import type { PaymentMethod } from "@/lib/api/pos"

interface CartProps {
  state: CartState
  dispatch: React.Dispatch<CartAction>
  onCheckout: (method: PaymentMethod) => void
}

const PAYMENT_METHODS: { method: PaymentMethod; label: string; className: string }[] = [
  { method: "cash", label: "现金", className: "bg-emerald-500 hover:bg-emerald-600 text-white" },
  { method: "wechat", label: "微信", className: "bg-green-500 hover:bg-green-600 text-white" },
  { method: "alipay", label: "支付宝", className: "bg-blue-500 hover:bg-blue-600 text-white" },
]

/**
 * Cart shows the POS shopping cart items, real-time totals, and checkout buttons.
 * All monetary calculations use decimal.js to avoid floating-point errors.
 */
export function Cart({ state, dispatch, onCheckout }: CartProps) {
  const subtotal = cartTotal(state.items)
  const total = cartNetTotal(state)
  const isEmpty = state.items.length === 0

  return (
    <div className="flex h-full flex-col rounded-xl border border-border bg-card">
      {/* Cart items */}
      <div className="flex-1 overflow-y-auto p-3">
        {isEmpty ? (
          <div className="flex h-full items-center justify-center py-12 text-sm text-muted-foreground">
            购物车为空
          </div>
        ) : (
          <div className="flex flex-col gap-1">
            {state.items.map((item) => {
              const lineTotal = item.unitPrice.times(item.quantity)
              return (
                <div
                  key={item.productId}
                  className="flex items-center gap-2 rounded-lg border border-border/50 bg-background p-2"
                >
                  {/* Product name */}
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{item.productName}</p>
                    <p className="text-xs text-muted-foreground">
                      ¥{item.unitPrice.toFixed(2)} / {item.unitName}
                    </p>
                  </div>

                  {/* Qty controls */}
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() =>
                        dispatch({
                          type: "SET_QUANTITY",
                          productId: item.productId,
                          quantity: item.quantity.minus(1),
                        })
                      }
                      className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-sm hover:bg-muted"
                    >
                      −
                    </button>
                    <input
                      type="number"
                      value={item.quantity.toString()}
                      onChange={(e) => {
                        const v = e.target.value
                        if (v && !isNaN(Number(v))) {
                          dispatch({
                            type: "SET_QUANTITY",
                            productId: item.productId,
                            quantity: new Decimal(v),
                          })
                        }
                      }}
                      className="h-7 w-12 rounded-md border border-border bg-background text-center text-sm outline-none focus:ring-1 focus:ring-ring"
                      data-pos-qty={item.productId}
                    />
                    <button
                      onClick={() =>
                        dispatch({
                          type: "SET_QUANTITY",
                          productId: item.productId,
                          quantity: item.quantity.plus(1),
                        })
                      }
                      className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-sm hover:bg-muted"
                    >
                      +
                    </button>
                  </div>

                  {/* Line total */}
                  <div className="w-16 text-right text-sm tabular-nums">
                    ¥{lineTotal.toFixed(2)}
                  </div>

                  {/* Remove */}
                  <button
                    aria-label={`删除 ${item.productName}`}
                    onClick={() =>
                      dispatch({ type: "REMOVE_ITEM", productId: item.productId })
                    }
                    className="flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                  >
                    ×
                  </button>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Discount + remark */}
      {!isEmpty && (
        <div className="border-t border-border px-3 py-2">
          <div className="flex items-center gap-2">
            <label className="text-xs text-muted-foreground shrink-0">
              {state.discountType === "percent" ? "折扣 %" : "优惠 ¥"}
            </label>
            <input
              type="number"
              min="0"
              value={state.discount.toString()}
              onChange={(e) => {
                const v = e.target.value
                if (!isNaN(Number(v))) {
                  dispatch({
                    type: "APPLY_DISCOUNT",
                    amount: new Decimal(v || "0"),
                    discountType: state.discountType,
                  })
                }
              }}
              className="h-7 w-20 rounded-md border border-border bg-background px-2 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
            <input
              type="text"
              placeholder="备注"
              value={state.remark}
              onChange={(e) =>
                dispatch({ type: "SET_REMARK", remark: e.target.value })
              }
              className="h-7 flex-1 rounded-md border border-border bg-background px-2 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
        </div>
      )}

      {/* Total */}
      <div className="border-t border-border px-4 py-3 text-right">
        {!state.discount.isZero() && (
          <div className="mb-1 text-sm text-muted-foreground">
            小计 <span className="tabular-nums">¥{subtotal.toFixed(2)}</span>
          </div>
        )}
        <div className="text-2xl font-bold tabular-nums text-emerald-600">
          ¥{total.toFixed(2)}
        </div>
      </div>

      {/* Checkout buttons */}
      <div className="border-t border-border p-3">
        <div className="grid grid-cols-3 gap-2">
          {PAYMENT_METHODS.map(({ method, label, className }) => (
            <button
              key={method}
              disabled={isEmpty}
              onClick={() => onCheckout(method)}
              data-pos-pay-cash={method === "cash" ? true : undefined}
              className={`h-16 rounded-lg text-base font-semibold transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${className}`}
            >
              {label}
            </button>
          ))}
        </div>
        <div className="mt-2 grid grid-cols-2 gap-2">
          <button
            disabled={isEmpty}
            onClick={() => onCheckout("credit")}
            className="h-10 rounded-lg border border-orange-400 text-sm font-medium text-orange-600 hover:bg-orange-50 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            赊账
          </button>
          <button
            disabled={isEmpty}
            onClick={() => dispatch({ type: "CLEAR_CART" })}
            className="h-10 rounded-lg border border-border text-sm font-medium text-muted-foreground hover:bg-muted disabled:opacity-40 disabled:cursor-not-allowed"
          >
            清空
          </button>
        </div>
      </div>
    </div>
  )
}
