"use client"

/**
 * PaymentForm — inline form for recording a partial/full payment on a sale bill.
 * Displays existing payments, running paid total, and remaining receivable.
 */

import { useState } from "react"
import { recordPayment, PAY_TYPE_LABEL, type Payment } from "@/lib/api/payment"

const PAY_METHODS = Object.entries(PAY_TYPE_LABEL).map(([value, label]) => ({
  value,
  label,
}))

interface Props {
  billId: string
  receivableAmount: string
  payments: Payment[]
  tenantId?: string
  onSuccess: (payment: Payment) => void
}

export function PaymentForm({
  billId,
  receivableAmount,
  payments,
  tenantId,
  onSuccess,
}: Props) {
  const [amount, setAmount] = useState("")
  const [method, setMethod] = useState("cash")
  const [remark, setRemark] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const remaining = parseFloat(receivableAmount) || 0

  const inputCls =
    "w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const amt = parseFloat(amount)
    if (!amt || amt <= 0) {
      setError("请输入有效的收款金额")
      return
    }
    if (amt > remaining + 0.001) {
      setError(`收款金额不能超过应收 ¥${remaining.toFixed(2)}`)
      return
    }

    setSaving(true)
    setError(null)
    try {
      const payment = await recordPayment(
        {
          bill_id: billId,
          amount: String(amt),
          payment_method: method,
          remark: remark || undefined,
        },
        tenantId
      )
      setAmount("")
      setRemark("")
      onSuccess(payment)
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      {/* Payment history */}
      {payments.length > 0 && (
        <div className="rounded-lg border border-border overflow-hidden">
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
                  <td className="px-4 py-2 text-muted-foreground text-xs">
                    {new Date(p.pay_date).toLocaleDateString("zh-CN")}
                  </td>
                  <td className="px-4 py-2">
                    {PAY_TYPE_LABEL[p.pay_type] ?? p.pay_type}
                  </td>
                  <td className="px-4 py-2 text-right font-mono">{p.amount}</td>
                  <td className="px-4 py-2 text-muted-foreground text-xs">
                    {p.remark ?? "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Receivable summary */}
      <div className="flex items-center gap-4 text-sm">
        <span className="text-muted-foreground">待收金额</span>
        <span
          className={`font-mono font-semibold ${
            remaining > 0 ? "text-amber-600" : "text-green-600"
          }`}
        >
          ¥ {remaining.toFixed(2)}
        </span>
      </div>

      {/* Record new payment */}
      {remaining > 0 && (
        <form onSubmit={handleSubmit} className="space-y-3">
          <h3 className="text-sm font-medium">登记收款</h3>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">支付方式</label>
              <select
                className={inputCls}
                value={method}
                onChange={(e) => setMethod(e.target.value)}
              >
                {PAY_METHODS.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">金额</label>
              <input
                type="number"
                className={inputCls}
                value={amount}
                placeholder={remaining.toFixed(2)}
                min="0.01"
                step="0.01"
                onChange={(e) => setAmount(e.target.value)}
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">备注</label>
              <input
                type="text"
                className={inputCls}
                value={remark}
                placeholder="可选"
                onChange={(e) => setRemark(e.target.value)}
              />
            </div>
          </div>

          {error && (
            <div className="rounded-md bg-destructive/10 border border-destructive/30 px-3 py-2 text-xs text-destructive">
              {error}
            </div>
          )}

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={saving}
              className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-60"
            >
              {saving ? "处理中..." : "确认收款"}
            </button>
          </div>
        </form>
      )}

      {remaining <= 0 && payments.length > 0 && (
        <div className="rounded-md bg-green-500/10 border border-green-500/30 px-4 py-2 text-sm text-green-700">
          该销售单已结清
        </div>
      )}
    </div>
  )
}
