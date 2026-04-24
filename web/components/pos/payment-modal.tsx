"use client"

import React, { useState, useEffect, useRef } from "react"
import Decimal from "decimal.js"
import type { PaymentMethod } from "@/lib/api/pos"

export type PaymentMode = "cash" | "wechat" | "alipay" | "credit"

export interface PaymentConfirmArgs {
  paymentMethod: PaymentMethod
  paidAmount: Decimal
  customerName?: string
}

interface PaymentModalProps {
  open: boolean
  mode: PaymentMode
  totalAmount: Decimal
  onConfirm: (args: PaymentConfirmArgs) => void
  onClose: () => void
}

const MODE_LABELS: Record<PaymentMode, string> = {
  cash: "现金收款",
  wechat: "微信收款",
  alipay: "支付宝收款",
  credit: "赊账",
}

/**
 * PaymentModal handles payment confirmation for the POS checkout flow.
 * Supports cash (with change calculation), WeChat, Alipay (QR placeholder), and credit.
 */
export function PaymentModal({
  open,
  mode,
  totalAmount,
  onConfirm,
  onClose,
}: PaymentModalProps) {
  const [paidAmountStr, setPaidAmountStr] = useState("")
  const [customerName, setCustomerName] = useState("")
  const cashInputRef = useRef<HTMLInputElement>(null)

  // Reset when mode or open changes
  useEffect(() => {
    if (open) {
      setPaidAmountStr(totalAmount.toFixed(2))
      setCustomerName("")
      if (mode === "cash") {
        // Use setTimeout to allow DOM to render before focusing
        setTimeout(() => cashInputRef.current?.focus(), 50)
      }
    }
  }, [open, mode, totalAmount])

  // Close on ESC
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, onClose])

  if (!open) return null

  const paidAmount = paidAmountStr ? new Decimal(paidAmountStr).abs() : new Decimal(0)
  const change = paidAmount.minus(totalAmount)
  const isNegativeChange = change.lt(0)

  const handleCashConfirm = () => {
    if (paidAmountStr) {
      onConfirm({ paymentMethod: "cash", paidAmount })
    }
  }

  const handleQrConfirm = (method: "wechat" | "alipay") => {
    onConfirm({ paymentMethod: method, paidAmount: totalAmount })
  }

  const handleCreditConfirm = () => {
    onConfirm({
      paymentMethod: "credit",
      paidAmount: new Decimal(0),
      customerName: customerName || undefined,
    })
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      role="dialog"
      aria-modal="true"
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50"
        onClick={onClose}
      />

      {/* Panel */}
      <div className="relative z-10 w-full max-w-sm rounded-xl bg-background p-6 shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{MODE_LABELS[mode]}</h2>
          <button
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground"
            aria-label="关闭"
          >
            ×
          </button>
        </div>

        {/* Total display */}
        <div className="mb-4 rounded-lg bg-muted p-4 text-center">
          <div className="text-sm text-muted-foreground">应收金额</div>
          <div className="mt-1 text-4xl font-bold tabular-nums text-emerald-600">
            ¥{totalAmount.toFixed(2)}
          </div>
        </div>

        {/* Cash mode */}
        {mode === "cash" && (
          <div className="flex flex-col gap-3">
            <div>
              <label htmlFor="paid-amount" className="mb-1 block text-sm font-medium">
                实收金额
              </label>
              <input
                id="paid-amount"
                ref={cashInputRef}
                type="number"
                min="0"
                step="0.01"
                value={paidAmountStr}
                onChange={(e) => setPaidAmountStr(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleCashConfirm()
                }}
                className="w-full rounded-lg border border-border bg-background px-3 py-2 text-xl outline-none focus:ring-2 focus:ring-ring tabular-nums"
                aria-label="实收金额"
              />
            </div>

            {/* Change calculation */}
            <div className="flex justify-between rounded-lg border border-border px-4 py-2 text-sm">
              <span className="text-muted-foreground">找零</span>
              <span
                data-testid="change-amount"
                className={`font-semibold tabular-nums ${
                  isNegativeChange ? "text-red-500" : "text-foreground"
                }`}
              >
                ¥{change.toFixed(2)}
              </span>
            </div>

            {isNegativeChange && (
              <p className="text-xs text-red-500">实收金额不足，请补足差额</p>
            )}

            <button
              onClick={handleCashConfirm}
              disabled={!paidAmountStr || isNegativeChange}
              className="h-12 w-full rounded-lg bg-emerald-500 text-base font-semibold text-white hover:bg-emerald-600 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              确认收款
            </button>
          </div>
        )}

        {/* WeChat / Alipay mode */}
        {(mode === "wechat" || mode === "alipay") && (
          <div className="flex flex-col items-center gap-4">
            <div className="flex h-48 w-48 items-center justify-center rounded-lg border-2 border-dashed border-border bg-muted text-sm text-muted-foreground">
              二维码占位区
              <br />
              200×200
            </div>
            <p className="text-sm text-muted-foreground">
              请扫描{mode === "wechat" ? "微信" : "支付宝"}付款码完成支付
            </p>
            <button
              onClick={() => handleQrConfirm(mode)}
              className="h-12 w-full rounded-lg bg-primary text-base font-semibold text-primary-foreground hover:opacity-90"
            >
              已收款
            </button>
          </div>
        )}

        {/* Credit mode */}
        {mode === "credit" && (
          <div className="flex flex-col gap-3">
            <div>
              <label htmlFor="customer-name" className="mb-1 block text-sm font-medium">
                客户姓名
              </label>
              <input
                id="customer-name"
                type="text"
                value={customerName}
                onChange={(e) => setCustomerName(e.target.value)}
                placeholder="请输入客户姓名"
                className="w-full rounded-lg border border-border bg-background px-3 py-2 outline-none focus:ring-2 focus:ring-ring"
                aria-label="客户姓名"
              />
            </div>
            <button
              onClick={handleCreditConfirm}
              className="h-12 w-full rounded-lg bg-orange-500 text-base font-semibold text-white hover:bg-orange-600"
            >
              确认赊账
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
