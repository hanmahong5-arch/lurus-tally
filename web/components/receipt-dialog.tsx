"use client"

/**
 * ReceiptDialog — take one lump sum from a customer (or pay one to a supplier) and let it
 * settle the oldest unpaid bills first. The list of bills the money would cover is read from
 * the server before anything is saved; this component never works the split out itself.
 */

import { useEffect, useState } from "react"
import Decimal from "decimal.js"
import { toast } from "sonner"
import { createReceipt, previewReceipt, type ReceiptDirection, type ReceiptResult } from "@/lib/api/receipts"
import { PAY_TYPE_LABEL } from "@/lib/api/payment"
import { ApiError, extractApiError, NetworkError } from "@/lib/api/errors"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { formatCNY, formatDate } from "@/lib/format"
import { useSubmitKey } from "@/lib/submit-key"

// "赊账" is the absence of a payment, so it is not offered when money actually changes hands.
const PAY_METHODS = Object.entries(PAY_TYPE_LABEL)
  .filter(([value]) => value !== "credit")
  .map(([value, label]) => ({ value, label }))

const COPY = {
  ar: {
    title: "收款",
    who: "客户",
    owed: "现在欠款",
    amount: "收到金额",
    method: "收款方式",
    submit: "确认收款",
    done: "已收款",
    cover: "这笔钱会先冲掉最早的欠款：",
    outstanding: "欠",
  },
  ap: {
    title: "付款",
    who: "供应商",
    owed: "现在欠他们",
    amount: "付出金额",
    method: "付款方式",
    submit: "确认付款",
    done: "已付款",
    cover: "这笔钱会先付清最早的进货款：",
    outstanding: "欠",
  },
} as const

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

const MONEY_RE = /^\d+(\.\d{1,2})?$/

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  direction: ReceiptDirection
  partnerId: string
  partnerName: string
  /** Current total owed, used to prefill the amount. */
  owed?: string
  tenantId?: string
  /** Called after the receipt is saved; the page re-reads its balances here. */
  onDone: () => void | Promise<void>
}

/**
 * 旧单据不足一分的尾差说明。diff = 记到单据上的钱 - 这些单据精确的欠款，正数是多记、负数是少记；
 * 金额一律向下取到分，不把不足一分的零头放大成一分。
 */
function tailNoteOf(diff: string | undefined): string {
  const tail = new Decimal(diff ?? 0)
  if (tail.isZero()) return ""
  const shown = tail.abs().toDecimalPlaces(2, Decimal.ROUND_DOWN)
  const amountText = shown.isZero() ? "不足一分钱" : formatCNY(shown.toFixed(2))
  return tail.gt(0)
    ? `，旧单据另有 ${amountText} 零头，已一并记上`
    : `，旧单据有 ${amountText} 零头没收，已按结清处理`
}

export function ReceiptDialog({ open, onOpenChange, direction, partnerId, partnerName, owed, tenantId, onDone }: Props) {
  const copy = COPY[direction]
  const [amount, setAmount] = useState("")
  const [method, setMethod] = useState("cash")
  const [remark, setRemark] = useState("")
  const [preview, setPreview] = useState<ReceiptResult | null>(null)
  const [problem, setProblem] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [refresh, setRefresh] = useState(0)
  const { keyFor, failed, succeeded } = useSubmitKey()

  // Start from the full balance each time the dialog opens, the common month-end case.
  useEffect(() => {
    if (!open) return
    setAmount(owed && Number(owed) > 0 ? Number(owed).toFixed(2) : "")
    setRemark("")
    setPreview(null)
    setProblem(null)
  }, [open, owed, partnerId])

  const trimmed = amount.trim()
  const valid = MONEY_RE.test(trimmed) && Number(trimmed) > 0

  useEffect(() => {
    if (!open || !valid) {
      setPreview(null)
      setProblem(trimmed && !valid ? "请输入大于 0 的金额，最多两位小数" : null)
      return
    }
    const ctl = new AbortController()
    if (refresh > 0) setPreview(null)
    const timer = setTimeout(() => {
      previewReceipt(direction, partnerId, trimmed, { tenantId, signal: ctl.signal }).then(
        (r) => {
          setPreview(r)
          setProblem(null)
        },
        (e) => {
          if (ctl.signal.aborted) return
          setPreview(null)
          setProblem(extractApiError(e).message)
        }
      )
    }, 250)
    return () => {
      clearTimeout(timer)
      ctl.abort()
    }
  }, [open, valid, trimmed, direction, partnerId, tenantId, refresh])

  async function submit() {
    if (!preview || saving) return
    setSaving(true)
    const body = {
      direction,
      partner_id: partnerId,
      amount: trimmed,
      payment_method: method,
      remark: remark.trim() || undefined,
    }
    try {
      const res = await createReceipt(body, tenantId, keyFor(body))
      succeeded()
      const tailNote = tailNoteOf(res.diff)
      toast.success(`${copy.done} ${formatCNY(res.amount)}，涉及 ${res.allocations.length} 张单据${tailNote}`)
      onOpenChange(false)
      await onDone()
    } catch (e) {
      failed(e)
      // The split shown may be stale (someone else may have just settled a bill); a lost reply
      // keeps it so the retry stays the same request, anything else forces a fresh preview.
      const lostReply =
        e instanceof NetworkError || (e instanceof ApiError && [502, 503, 504].includes(e.status))
      if (!lostReply) setRefresh((n) => n + 1)
      setProblem(extractApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title={`${partnerName} · ${copy.title}`}
      className="max-w-lg"
      footer={
        <>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={!preview || saving} onClick={() => void submit()}>
            {saving ? "保存中..." : copy.submit}
          </Button>
        </>
      }
    >
      <div className="mt-4 space-y-3 text-sm" data-testid="receipt-dialog">
        {owed !== undefined && (
          <div className="flex items-center gap-3">
            <span className="text-muted-foreground">{copy.owed}</span>
            <span className="font-mono font-semibold tabular-nums text-warning">{formatCNY(owed)}</span>
          </div>
        )}
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="receipt-amount">{copy.amount}</Label>
            <Input
              id="receipt-amount"
              inputMode="decimal"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="0.00"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="receipt-method">{copy.method}</Label>
            <select id="receipt-method" className={SELECT_CLASS} value={method} onChange={(e) => setMethod(e.target.value)}>
              {PAY_METHODS.map((m) => (
                <option key={m.value} value={m.value}>
                  {m.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="receipt-remark">备注</Label>
          <Input id="receipt-remark" value={remark} onChange={(e) => setRemark(e.target.value)} placeholder="可选" maxLength={500} />
        </div>

        {problem && (
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/5 p-2.5 text-destructive">
            {problem}
          </p>
        )}

        {preview && (
          <div>
            <p className="mb-1.5 text-muted-foreground">{copy.cover}</p>
            <div className="max-h-56 overflow-auto rounded-lg border border-border">
              <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground">
                  <tr>
                    <th className="px-3 py-1.5 text-left font-medium">单号</th>
                    <th className="px-3 py-1.5 text-left font-medium">日期</th>
                    <th className="px-3 py-1.5 text-right font-medium">{copy.outstanding}</th>
                    <th className="px-3 py-1.5 text-right font-medium">本次冲</th>
                    <th className="px-3 py-1.5 text-right font-medium">还剩</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {preview.allocations.map((a) => (
                    <tr key={a.bill_id}>
                      <td className="px-3 py-1.5">{a.bill_no}</td>
                      <td className="px-3 py-1.5 text-xs text-muted-foreground">{formatDate(a.bill_date)}</td>
                      <td className="px-3 py-1.5 text-right font-mono tabular-nums">{formatCNY(a.outstanding)}</td>
                      <td className="px-3 py-1.5 text-right font-mono tabular-nums">{formatCNY(a.applied)}</td>
                      <td className="px-3 py-1.5 text-right font-mono tabular-nums">
                        {Number(a.remaining) > 0 ? formatCNY(a.remaining) : "已结清"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>
    </Modal>
  )
}
