"use client"

/**
 * PaymentForm — inline form for recording a partial/full payment on a bill: money received
 * on a sale ("receive") or money paid on a purchase ("pay").
 * Displays existing payments and what is still outstanding. It never computes the new
 * figures itself: after a successful save the page re-reads the bill (onSuccess).
 */

import { useState } from "react"
import { recordPayment, PAY_TYPE_LABEL, type Payment } from "@/lib/api/payment"
import { extractApiError } from "@/lib/api/errors"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { formatCNY, formatDate } from "@/lib/format"
import { roundCents } from "@/lib/cents"
import { cn } from "@/lib/utils"

// "赊账" is the absence of a payment, so it is not offered when money actually changes hands.
const PAY_METHODS = Object.entries(PAY_TYPE_LABEL)
  .filter(([value]) => value !== "credit")
  .map(([value, label]) => ({ value, label }))

const COPY = {
  receive: {
    title: "登记收款",
    method: "收款方式",
    owed: "待收",
    outstanding: "待收金额",
    submit: "确认收款",
    invalid: "请输入有效的收款金额",
    settled: "该销售单已结清",
  },
  pay: {
    title: "登记付款",
    method: "付款方式",
    owed: "待付",
    outstanding: "待付金额",
    submit: "确认付款",
    invalid: "请输入有效的付款金额",
    settled: "该采购单已付清",
  },
} as const

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

interface Props {
  billId: string
  /** Still owed on the bill (after returns), string decimal. */
  outstandingAmount: string
  payments: Payment[]
  tenantId?: string
  /** "receive" = customer pays us (sale), "pay" = we pay a supplier (purchase). */
  kind?: "receive" | "pay"
  /** Called after the payment is saved; the page re-reads the bill here. */
  onSuccess: () => void | Promise<void>
}

export function PaymentForm({ billId, outstandingAmount, payments, tenantId, kind = "receive", onSuccess }: Props) {
  const copy = COPY[kind]
  const [amount, setAmount] = useState("")
  const [method, setMethod] = useState("cash")
  const [remark, setRemark] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // A bill booked before totals were kept to the cent can owe a fraction of one; the form
  // works in whole cents, and the server settles the bill when the last payment is that figure.
  const remaining = roundCents(parseFloat(outstandingAmount) || 0)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const amt = parseFloat(amount)
    if (!amt || amt <= 0) {
      setError(copy.invalid)
      return
    }
    if (amt > remaining + 0.001) {
      setError(`本次 ${formatCNY(amt)} 超过${copy.owed} ${formatCNY(remaining)}`)
      return
    }

    setSaving(true)
    setError(null)
    try {
      await recordPayment(
        { bill_id: billId, amount: amount.trim(), payment_method: method, remark: remark || undefined },
        tenantId
      )
      setAmount("")
      setRemark("")
      await onSuccess()
    } catch (e) {
      setError(extractApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      {/* Payment history */}
      {payments.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-4 py-2 text-left font-medium">日期</th>
                <th className="px-4 py-2 text-left font-medium">方式</th>
                <th className="px-4 py-2 text-right font-medium">金额</th>
                <th className="px-4 py-2 text-left font-medium">备注</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {payments.map((p) => (
                <tr key={p.id} className="hover:bg-muted/20">
                  <td className="px-4 py-2 text-xs text-muted-foreground">
                    {formatDate(p.pay_date)}
                  </td>
                  <td className="px-4 py-2">{p.pay_type === "credit" && p.remark?.startsWith("采购退货冲抵") ? "退货冲抵" : (PAY_TYPE_LABEL[p.pay_type] ?? p.pay_type)}</td>
                  <td className="px-4 py-2 text-right font-mono tabular-nums">{formatCNY(parseFloat(p.amount))}</td>
                  <td className="px-4 py-2 text-xs text-muted-foreground">{p.remark ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Receivable summary */}
      <div className="flex items-center gap-4 text-sm">
        <span className="text-muted-foreground">{copy.outstanding}</span>
        <span className={cn("font-mono font-semibold tabular-nums", remaining > 0 ? "text-warning" : "text-success")}>
          {formatCNY(remaining)}
        </span>
      </div>

      {/* Record new payment */}
      {remaining > 0 && (
        <form onSubmit={handleSubmit} className="space-y-3">
          <h3 className="text-sm font-medium">{copy.title}</h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label htmlFor="pay-method">{copy.method}</Label>
              <select id="pay-method" className={SELECT_CLASS} value={method} onChange={(e) => setMethod(e.target.value)}>
                {PAY_METHODS.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pay-amount">金额</Label>
              <div className="flex gap-2">
                <Input
                  id="pay-amount"
                  type="number"
                  value={amount}
                  placeholder={`最多 ${formatCNY(remaining)}`}
                  min="0.01"
                  step="0.01"
                  onChange={(e) => setAmount(e.target.value)}
                />
                <Button type="button" variant="outline" size="sm" onClick={() => setAmount(remaining.toFixed(2))}>
                  全额
                </Button>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pay-remark">备注</Label>
              <Input
                id="pay-remark"
                type="text"
                value={remark}
                placeholder="可选"
                onChange={(e) => setRemark(e.target.value)}
              />
            </div>
          </div>

          {error && <ErrorBanner>{error}</ErrorBanner>}

          <div className="flex justify-end">
            <Button type="submit" disabled={saving}>
              {saving ? "处理中..." : copy.submit}
            </Button>
          </div>
        </form>
      )}

      {remaining <= 0 && payments.length > 0 && (
        <div className="rounded-md border border-success/30 bg-success/10 px-4 py-2 text-sm text-success">
          {copy.settled}
        </div>
      )}
    </div>
  )
}
