"use client"

import React, { useState, useEffect, useRef } from "react"
import Decimal from "decimal.js"
import { formatCNY } from "@/lib/format"
import type { PaymentMethod } from "@/lib/api/pos"
import { CustomerPicker } from "@/components/entity-pickers"
import { LOCKED_HINT, useSubscriptionLocked } from "@/hooks/use-subscription-locked"

export type PaymentMode = "cash" | "wechat" | "alipay" | "credit"

export interface PaymentConfirmArgs {
  paymentMethod: PaymentMethod
  paidAmount: Decimal
  /** Customer profile the credit sale is booked to; always set for credit. */
  partnerId?: string
  customerName?: string
  /** Free-text note for the bill, e.g. the last digits of a wechat / alipay receipt. */
  note?: string
}

interface PaymentModalProps {
  open: boolean
  mode: PaymentMode
  totalAmount: Decimal
  /**
   * When true the confirm button is disabled and shows "处理中...", and ESC /
   * backdrop-click are ignored. Prevents double-submit while quickCheckout is
   * in flight, complementing the server-side Idempotency-Key dedup.
   */
  loading?: boolean
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
 * Supports cash (with change calculation), WeChat / Alipay (cashier confirms the money
 * arrived, optionally noting the receipt's last digits), and credit (a customer is required).
 */
export function PaymentModal({
  open,
  mode,
  totalAmount,
  loading = false,
  onConfirm,
  onClose,
}: PaymentModalProps) {
  const [paidAmountStr, setPaidAmountStr] = useState("")
  const [customerId, setCustomerId] = useState("")
  const [customerName, setCustomerName] = useState("")
  const [tail, setTail] = useState("")
  const cashInputRef = useRef<HTMLInputElement>(null)
  const locked = useSubscriptionLocked()

  // The parent builds a fresh Decimal every render; key the reset on its string form so a
  // re-render during a failed checkout does not wipe the cashier's input (it would change
  // the request fingerprint and drop the Idempotency-Key).
  const totalStr = totalAmount.toFixed(2)

  // Reset when mode, open or the payable amount changes
  useEffect(() => {
    if (open) {
      setPaidAmountStr(totalStr)
      setCustomerId("")
      setCustomerName("")
      setTail("")
      if (mode === "cash") {
        // Use setTimeout to allow DOM to render before focusing
        setTimeout(() => cashInputRef.current?.focus(), 50)
      }
    }
  }, [open, mode, totalStr])

  // Close on ESC — disabled while loading so an in-flight checkout cannot be
  // dismissed mid-request (the server still finishes; the client would lose
  // the result).
  useEffect(() => {
    if (!open || loading) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, loading, onClose])

  if (!open) return null

  const paidAmount = paidAmountStr ? new Decimal(paidAmountStr).abs() : new Decimal(0)
  const change = paidAmount.minus(totalAmount)
  const isNegativeChange = change.lt(0)

  const handleCashConfirm = () => {
    if (locked) return
    if (paidAmountStr) {
      onConfirm({ paymentMethod: "cash", paidAmount })
    }
  }

  const handleQrConfirm = (method: "wechat" | "alipay") => {
    if (locked) return
    const digits = tail.trim()
    onConfirm({
      paymentMethod: method,
      paidAmount: totalAmount,
      note: digits ? `${method === "wechat" ? "微信" : "支付宝"}流水尾号 ${digits}` : undefined,
    })
  }

  const handleCreditConfirm = () => {
    if (locked || !customerId) return
    onConfirm({
      paymentMethod: "credit",
      paidAmount: new Decimal(0),
      partnerId: customerId,
      customerName: customerName || undefined,
    })
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-3"
      role="dialog"
      aria-modal="true"
    >
      {/* Backdrop — click-to-close is disabled while loading. */}
      <div
        className="absolute inset-0 bg-black/50"
        onClick={loading ? undefined : onClose}
      />

      {/* Panel */}
      <div className="relative z-10 w-full max-w-sm rounded-xl bg-background p-6 shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{MODE_LABELS[mode]}</h2>
          <button
            onClick={onClose}
            disabled={loading}
            className="text-muted-foreground hover:text-foreground disabled:opacity-50 disabled:cursor-not-allowed"
            aria-label="关闭"
          >
            ×
          </button>
        </div>

        {/* Total display */}
        <div className="mb-4 rounded-lg bg-muted p-4 text-center">
          <div className="text-sm text-muted-foreground">应收金额</div>
          <div className="mt-1 text-4xl font-bold tabular-nums text-emerald-600">
            {formatCNY(totalAmount.toNumber())}
          </div>
        </div>

        {locked && (
          <p role="alert" className="mb-3 rounded-lg bg-amber-100 px-3 py-2 text-sm text-amber-950">
            {LOCKED_HINT}
          </p>
        )}

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
                {formatCNY(change.toNumber())}
              </span>
            </div>

            {isNegativeChange && (
              <p className="text-xs text-red-500">实收金额不足，请补足差额</p>
            )}

            <button
              onClick={handleCashConfirm}
              disabled={!paidAmountStr || isNegativeChange || loading || locked}
              className="h-12 w-full rounded-lg bg-emerald-500 text-base font-semibold text-white hover:bg-emerald-600 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {loading ? "处理中..." : "确认收款"}
            </button>
          </div>
        )}

        {/* WeChat / Alipay mode */}
        {(mode === "wechat" || mode === "alipay") && (
          <div className="flex flex-col gap-3">
            <p className="text-sm text-muted-foreground">
              请让顾客用{mode === "wechat" ? "微信" : "支付宝"}付款，确认到账后再点下面的按钮。
            </p>
            <div>
              <label htmlFor="pay-tail" className="mb-1 block text-sm font-medium">
                流水尾号（选填）
              </label>
              <input
                id="pay-tail"
                type="text"
                inputMode="numeric"
                maxLength={12}
                value={tail}
                onChange={(e) => setTail(e.target.value)}
                placeholder="方便日后对账，可不填"
                className="w-full rounded-lg border border-border bg-background px-3 py-2 outline-none focus:ring-2 focus:ring-ring"
                aria-label="流水尾号"
              />
            </div>
            <button
              onClick={() => handleQrConfirm(mode)}
              disabled={loading || locked}
              className="h-12 w-full rounded-lg bg-primary text-base font-semibold text-primary-foreground hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {loading ? "处理中..." : "已收到顾客付款"}
            </button>
          </div>
        )}

        {/* Credit mode */}
        {mode === "credit" && (
          <div className="flex flex-col gap-3">
            <div>
              <label htmlFor="credit-customer" className="mb-1 block text-sm font-medium">
                客户
              </label>
              <CustomerPicker
                id="credit-customer"
                value={customerId}
                label={customerName}
                placeholder="搜索客户，没有就直接新建"
                disabled={loading}
                onSelect={(c) => {
                  setCustomerId(c.id)
                  setCustomerName(c.name)
                }}
                onClear={() => {
                  setCustomerId("")
                  setCustomerName("")
                }}
              />
              {!customerId && (
                <p className="mt-1 text-xs text-orange-600">赊账必须选择客户，欠款才会记到他名下</p>
              )}
            </div>
            <button
              onClick={handleCreditConfirm}
              disabled={loading || locked || !customerId}
              className="h-12 w-full rounded-lg bg-orange-500 text-base font-semibold text-white hover:bg-orange-600 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {loading ? "处理中..." : "确认赊账"}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
