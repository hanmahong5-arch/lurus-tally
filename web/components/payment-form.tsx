"use client"

/**
 * PaymentForm — inline form for recording a partial/full payment on a sale bill.
 * Displays existing payments, running paid total, and remaining receivable.
 */

import { useState } from "react"
import { recordPayment, PAY_TYPE_LABEL, type Payment } from "@/lib/api/payment"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { formatCNY } from "@/lib/format"
import { cn } from "@/lib/utils"

const PAY_METHODS = Object.entries(PAY_TYPE_LABEL).map(([value, label]) => ({ value, label }))

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

interface Props {
  billId: string
  receivableAmount: string
  payments: Payment[]
  tenantId?: string
  onSuccess: (payment: Payment) => void
}

export function PaymentForm({ billId, receivableAmount, payments, tenantId, onSuccess }: Props) {
  const [amount, setAmount] = useState("")
  const [method, setMethod] = useState("cash")
  const [remark, setRemark] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const remaining = parseFloat(receivableAmount) || 0

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const amt = parseFloat(amount)
    if (!amt || amt <= 0) {
      setError("请输入有效的收款金额")
      return
    }
    if (amt > remaining + 0.001) {
      setError(`收款金额不能超过应收 ${formatCNY(remaining)}`)
      return
    }

    setSaving(true)
    setError(null)
    try {
      const payment = await recordPayment(
        { bill_id: billId, amount: String(amt), payment_method: method, remark: remark || undefined },
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
                    {new Date(p.pay_date).toLocaleDateString("zh-CN")}
                  </td>
                  <td className="px-4 py-2">{PAY_TYPE_LABEL[p.pay_type] ?? p.pay_type}</td>
                  <td className="px-4 py-2 text-right font-mono tabular-nums">{p.amount}</td>
                  <td className="px-4 py-2 text-xs text-muted-foreground">{p.remark ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Receivable summary */}
      <div className="flex items-center gap-4 text-sm">
        <span className="text-muted-foreground">待收金额</span>
        <span className={cn("font-mono font-semibold tabular-nums", remaining > 0 ? "text-warning" : "text-success")}>
          {formatCNY(remaining)}
        </span>
      </div>

      {/* Record new payment */}
      {remaining > 0 && (
        <form onSubmit={handleSubmit} className="space-y-3">
          <h3 className="text-sm font-medium">登记收款</h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label htmlFor="pay-method">支付方式</Label>
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
              <Input
                id="pay-amount"
                type="number"
                value={amount}
                placeholder={remaining.toFixed(2)}
                min="0.01"
                step="0.01"
                onChange={(e) => setAmount(e.target.value)}
              />
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
              {saving ? "处理中..." : "确认收款"}
            </Button>
          </div>
        </form>
      )}

      {remaining <= 0 && payments.length > 0 && (
        <div className="rounded-md border border-success/30 bg-success/10 px-4 py-2 text-sm text-success">
          该销售单已结清
        </div>
      )}
    </div>
  )
}
