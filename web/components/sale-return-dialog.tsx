"use client"

import { useEffect, useState } from "react"
import Decimal from "decimal.js"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { formatCNY } from "@/lib/format"
import { PAY_TYPE_LABEL } from "@/lib/api/payment"
import type { SaleBillItem } from "@/lib/api/sale"

// "赊账" is the absence of a payment: nobody is paid back by it.
const REFUND_METHODS = Object.entries(PAY_TYPE_LABEL)
  .filter(([value]) => value !== "credit")
  .map(([value, label]) => ({ value, label }))

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

/** What the sale has taken in so far; lets the dialog offer to hand money back with the goods. */
export interface RefundContext {
  /** Sale total, string decimal. */
  total: string
  /** Paid so far, string decimal. */
  paid: string
  /** Value of the goods already returned, string decimal. */
  returnedAmount: string
  /** How the customer paid, preselected as the way to pay them back. */
  defaultMethod: string
}

export interface RefundChoice {
  amount: string
  method: string
}

function dec(v: string | undefined): Decimal {
  try {
    const d = new Decimal(v || "0")
    return d.isFinite() ? d : new Decimal(0)
  } catch {
    return new Decimal(0)
  }
}

/** The most that can go back: paid beyond what the sale is still worth once the goods return. */
export function refundCeiling(ctx: RefundContext, returnValue: Decimal): Decimal {
  const net = Decimal.max(dec(ctx.total).minus(dec(ctx.returnedAmount)).minus(returnValue), 0)
  return Decimal.max(dec(ctx.paid).minus(net), 0).toDecimalPlaces(2, Decimal.ROUND_DOWN)
}

export interface SaleReturnDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  items: SaleBillItem[]
  /** product_id -> display name; unknown ids show a neutral label, never the id. */
  productNames: Record<string, string>
  submitting: boolean
  error: string | null
  /** When set, the clerk can hand money back together with the goods. */
  refund?: RefundContext
  onSubmit: (lines: { bill_item_id: string; qty: string }[], remark: string, refund?: RefundChoice) => void
}

function num(v: string | undefined): number {
  const n = parseFloat(v ?? "")
  return Number.isFinite(n) ? n : 0
}

/** Quantity still returnable on a line, trimmed to the 4 decimals the server keeps. */
export function returnableQty(item: SaleBillItem): number {
  return Math.max(0, Math.round((num(item.qty) - num(item.returned_qty)) * 10000) / 10000)
}

/**
 * Lets the clerk pick how many of each sold line the customer gives back. Only lines with
 * something left to return are listed; the server re-checks every quantity.
 */
export function SaleReturnDialog({
  open,
  onOpenChange,
  items,
  productNames,
  submitting,
  error,
  refund: refundCtx,
  onSubmit,
}: SaleReturnDialogProps) {
  const [qtys, setQtys] = useState<Record<string, string>>({})
  const [remark, setRemark] = useState("")
  const [refundOn, setRefundOn] = useState(false)
  // null = follow the suggested amount; set once the clerk types their own.
  const [refundText, setRefundText] = useState<string | null>(null)
  const [refundMethod, setRefundMethod] = useState("cash")

  useEffect(() => {
    if (open) {
      setQtys({})
      setRemark("")
      setRefundOn(false)
      setRefundText(null)
      setRefundMethod(refundCtx?.defaultMethod && refundCtx.defaultMethod !== "credit" ? refundCtx.defaultMethod : "cash")
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const rows = items.filter((it) => returnableQty(it) > 0)
  const picked = rows
    .map((it) => ({ it, qty: num(qtys[it.id]) }))
    .filter((r) => r.qty > 0)
  const tooMany = picked.find((r) => r.qty > returnableQty(r.it))
  const refund = picked.reduce((sum, r) => sum + r.qty * num(r.it.unit_price), 0)
  const returnValue = picked.reduce((sum, r) => sum.plus(new Decimal(r.qty).times(dec(r.it.unit_price))), new Decimal(0))
  const ceiling = refundCtx ? refundCeiling(refundCtx, returnValue) : new Decimal(0)
  const canRefund = !!refundCtx && picked.length > 0 && ceiling.gt(0)
  const refundAmount = refundOn && canRefund ? dec(refundText ?? ceiling.toFixed(2)) : new Decimal(0)
  const refundBad = refundOn && canRefund && (!refundAmount.gt(0) || refundAmount.gt(ceiling))

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="客户退货"
      description="填写客户这次退回的数量，确认后货会自动回到仓库，这张单的应收款也会相应减少。"
      className="max-w-lg"
      footer={
        <>
          <Button variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
            不退了
          </Button>
          <Button
            disabled={submitting || picked.length === 0 || !!tooMany || refundBad}
            onClick={() =>
              onSubmit(
                picked.map((r) => ({ bill_item_id: r.it.id, qty: String(r.qty) })),
                remark.trim(),
                refundOn && canRefund ? { amount: refundAmount.toFixed(2), method: refundMethod } : undefined
              )
            }
          >
            {submitting ? "处理中..." : "确认退货"}
          </Button>
        </>
      }
    >
      <div className="mt-4 space-y-3">
        {error && <ErrorBanner>{error}</ErrorBanner>}
        {rows.length === 0 && (
          <p className="py-2 text-sm text-muted-foreground">这张单的商品已经全部退完了，没有可退的了。</p>
        )}
        <ul className="space-y-3">
          {rows.map((it) => {
            const left = returnableQty(it)
            const over = num(qtys[it.id]) > left
            return (
              <li key={it.id} className="space-y-1">
                <div className="flex items-baseline justify-between gap-3 text-sm">
                  <span className="font-medium">{productNames[it.product_id] || "未知商品"}</span>
                  <span className="text-muted-foreground">
                    卖出 {it.qty}
                    {it.unit_name ?? ""}，最多还能退 {left}
                    {it.unit_name ?? ""}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    aria-label={`${productNames[it.product_id] || "商品"}退货数量`}
                    type="number"
                    inputMode="decimal"
                    min="0"
                    step="any"
                    placeholder="退几个"
                    value={qtys[it.id] ?? ""}
                    onChange={(e) => setQtys((q) => ({ ...q, [it.id]: e.target.value }))}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setQtys((q) => ({ ...q, [it.id]: String(left) }))}
                  >
                    全退
                  </Button>
                </div>
                {over && <p className="text-xs text-destructive">不能超过 {left}</p>}
              </li>
            )
          })}
        </ul>
        <div className="space-y-1.5">
          <Label htmlFor="return-remark">退货原因（选填）</Label>
          <Input
            id="return-remark"
            maxLength={200}
            placeholder="例如：质量问题、买多了"
            value={remark}
            onChange={(e) => setRemark(e.target.value)}
          />
        </div>
        {picked.length > 0 && (
          <p className="text-sm text-muted-foreground">
            本次退货金额：<span className="font-mono font-medium text-foreground">{formatCNY(refund)}</span>
            （按原来卖的单价算{refundCtx ? "" : "；已收的钱不会自动退，请和客户线下结算"}）
          </p>
        )}
        {canRefund && (
          <div className="space-y-2 rounded-lg border border-border p-3">
            <label className="flex items-center gap-2 text-sm font-medium">
              <input
                type="checkbox"
                checked={refundOn}
                onChange={(e) => setRefundOn(e.target.checked)}
              />
              同时退款 {formatCNY(ceiling.toNumber())}
            </label>
            {refundOn ? (
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <Label htmlFor="refund-amount">退给客户多少钱</Label>
                  <Input
                    id="refund-amount"
                    type="number"
                    inputMode="decimal"
                    min="0"
                    step="0.01"
                    value={refundText ?? ceiling.toFixed(2)}
                    onChange={(e) => setRefundText(e.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="refund-method">退款方式</Label>
                  <select
                    id="refund-method"
                    className={SELECT_CLASS}
                    value={refundMethod}
                    onChange={(e) => setRefundMethod(e.target.value)}
                  >
                    {REFUND_METHODS.map((m) => (
                      <option key={m.value} value={m.value}>
                        {m.label}
                      </option>
                    ))}
                  </select>
                </div>
                {refundBad && (
                  <p className="col-span-2 text-xs text-destructive">
                    退款要大于 0，且不能超过 {formatCNY(ceiling.toNumber())}（客户实际付过的钱）
                  </p>
                )}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">
                不勾选就只退货，已收的钱不动，请和客户线下结算。勾选后会记一笔退款，今天的收款会相应减少。
              </p>
            )}
          </div>
        )}
      </div>
    </Modal>
  )
}

/** A return whose goods are back in stock but whose refund did not get booked. */
export interface PendingRefund {
  /** The sale the money belongs to. */
  saleId: string
  /** The return it goes with; the server checks it and labels the payment with its number. */
  returnBillId: string
  amount: string
  method: string
}

export interface RefundRetryModalProps {
  pending: PendingRefund | null
  submitting: boolean
  error: string | null
  onRecord: (pending: PendingRefund) => void
  /** Leave it for later; the money has to be recorded by hand afterwards. */
  onClose: () => void
}

/** 补记退款: books the refund that failed with the return, so it counts in today's takings. */
export function RefundRetryModal({ pending, submitting, error, onRecord, onClose }: RefundRetryModalProps) {
  return (
    <Modal
      open={pending !== null}
      onOpenChange={(o) => {
        if (!o && !submitting) onClose()
      }}
      title="退货已完成，退款还没记上"
      description="货已经退回仓库。请先把钱当面退给客户，再点「补记退款」，这笔钱才会算进今日结单。"
      className="max-w-md"
      footer={
        <>
          <Button variant="outline" disabled={submitting} onClick={onClose}>
            稍后再说
          </Button>
          <Button disabled={submitting || !pending} onClick={() => pending && onRecord(pending)}>
            {submitting ? "处理中..." : "补记退款"}
          </Button>
        </>
      }
    >
      <div className="mt-4 space-y-3 text-sm">
        {error && <ErrorBanner>{error}</ErrorBanner>}
        {pending && (
          <p>
            要补记的退款：<span className="font-mono font-medium">{formatCNY(num(pending.amount))}</span>（
            {PAY_TYPE_LABEL[pending.method] ?? "现金"}）
          </p>
        )}
      </div>
    </Modal>
  )
}
