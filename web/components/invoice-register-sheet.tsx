"use client"

import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { Sheet } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Field } from "@/components/ui/field"
import { CustomerPicker, SupplierPicker } from "@/components/entity-pickers"
import { useTenantId } from "@/hooks/use-tenant-id"
import { listSaleBills } from "@/lib/api/sale"
import { listPurchaseBills } from "@/lib/api/purchase"
import {
  INVOICE_TYPES,
  DIRECTION_LABEL,
  createInvoice,
  invoiceTotal,
  moneyGt,
  parseMoney,
  remainingToInvoice,
  sumMoney,
  type Invoice,
  type InvoiceDirection,
} from "@/lib/api/invoices"
import { extractApiError } from "@/lib/api/errors"
import { shanghaiToday } from "@/lib/date-range"
import { TAX_RATES, splitTaxInclusive } from "@/lib/invoice-tax"
import { formatCNY, formatDate } from "@/lib/format"

/** A bill that can still take an invoice (approved, not fully invoiced). */
interface Candidate {
  id: string
  bill_no: string
  bill_date: string
  total_amount: string
  remaining: string
}

export interface InvoicePreset {
  direction: InvoiceDirection
  partnerId: string
  partnerName: string
  /** Bill ticked as soon as the candidate list loads. */
  billId?: string
  /** Facts about that bill, used when it is not among the loaded candidates. */
  bill?: { billNo: string; billDate: string; totalAmount: string; remaining: string }
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: (invoice: Invoice) => void
  preset?: InvoicePreset
}

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

const PAGE = 100

/**
 * Bill total after sales returns, two decimals. The list reports unpaid = (total - returns, floored
 * at zero) - paid, so net total = unpaid + paid; without those fields the plain total stands.
 */
function netTotal(b: { total_amount: string; paid_amount?: string; unpaid_amount?: string }): string {
  const unpaid = parseMoney(b.unpaid_amount ?? "")
  const paid = parseMoney(b.paid_amount ?? "")
  if (!unpaid || !paid) return b.total_amount
  return unpaid.plus(paid).toFixed(4)
}

/** 登记发票 drawer: header fields, auto total, and the bills this invoice covers. */
export function InvoiceRegisterSheet({ open, onOpenChange, onCreated, preset }: Props) {
  const tenantId = useTenantId()
  const [direction, setDirection] = useState<InvoiceDirection>(preset?.direction ?? "out")
  const [partner, setPartner] = useState({ id: preset?.partnerId ?? "", name: preset?.partnerName ?? "" })
  const [invoiceType, setInvoiceType] = useState<string>(INVOICE_TYPES[0])
  const [invoiceNo, setInvoiceNo] = useState("")
  const [issueDate, setIssueDate] = useState(() => shanghaiToday())
  const [excl, setExcl] = useState("")
  const [tax, setTax] = useState("")
  // Optional shortcut: pick a rate, type the tax-inclusive total, and excl / tax are worked out (still editable).
  const [rate, setRate] = useState("")
  const [inclusive, setInclusive] = useState("")
  const [remark, setRemark] = useState("")
  const [candidates, setCandidates] = useState<Candidate[]>([])
  const [loadingBills, setLoadingBills] = useState(false)
  const [presetMissing, setPresetMissing] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [billsError, setBillsError] = useState<string | null>(null)
  // billId -> allocated amount (string as typed)
  const [picked, setPicked] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [submitted, setSubmitted] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)

  // Opening from another entry point restarts from that entry's preset.
  useEffect(() => {
    if (!open) return
    setDirection(preset?.direction ?? "out")
    setPartner({ id: preset?.partnerId ?? "", name: preset?.partnerName ?? "" })
    setInvoiceType(INVOICE_TYPES[0])
    setInvoiceNo("")
    setIssueDate(shanghaiToday())
    setExcl("")
    setTax("")
    setRate("")
    setInclusive("")
    setRemark("")
    setPicked({})
    setSubmitted(false)
    setServerError(null)
  }, [open, preset])

  // Candidate bills: this partner's approved bills that are not fully invoiced.
  useEffect(() => {
    if (!open || !partner.id) {
      setCandidates([])
      setBillsError(null)
      setPresetMissing(false)
      setHasMore(false)
      return
    }
    const ctrl = new AbortController()
    setLoadingBills(true)
    setBillsError(null)
    const base = { partner_id: partner.id, status: 2 as const, size: PAGE, tenantId, signal: ctrl.signal }
    const fetchBills = (invoice_status: "none" | "partial") =>
      direction === "out"
        ? listSaleBills({ ...base, invoice_status })
        : listPurchaseBills({ ...base, invoice_status })
    // Two narrow queries (not yet / partly invoiced) so fully invoiced bills never crowd out older open ones.
    Promise.all([fetchBills("none"), fetchBills("partial")])
      .then(([a, b]) => {
        if (ctrl.signal.aborted) return
        const seen = new Set<string>()
        const merged = [...(a.items ?? []), ...(b.items ?? [])].filter((x) => {
          if (seen.has(x.id)) return false
          seen.add(x.id)
          return true
        })
        merged.sort((x, y) => (x.bill_date < y.bill_date ? 1 : x.bill_date > y.bill_date ? -1 : 0))
        const rows = merged
          .filter((b) => b.invoice_status !== "full")
          .map((b) => ({
            id: b.id,
            bill_no: b.bill_no,
            bill_date: b.bill_date,
            total_amount: b.total_amount,
            // A sale can only be invoiced for what stays sold: returns come off the total first.
            remaining: remainingToInvoice(netTotal(b), b.invoiced_amount),
          }))
          .filter((b) => moneyGt(b.remaining, "0"))
        setHasMore((a.total ?? 0) > PAGE || (b.total ?? 0) > PAGE)
        const want = preset?.billId
        if (want && partner.id === preset?.partnerId && direction === preset?.direction) {
          let hit = rows.find((r) => r.id === want)
          // Older than the loaded page: use the facts the entry point already knows.
          if (!hit && preset?.bill && moneyGt(preset.bill.remaining, "0")) {
            hit = {
              id: want,
              bill_no: preset.bill.billNo,
              bill_date: preset.bill.billDate,
              total_amount: preset.bill.totalAmount,
              remaining: preset.bill.remaining,
            }
            rows.unshift(hit)
          }
          if (hit) setPicked({ [hit.id]: hit.remaining })
          setPresetMissing(!hit)
        } else {
          setPresetMissing(false)
        }
        setCandidates(rows)
      })
      .catch((e) => {
        if (ctrl.signal.aborted) return
        setBillsError(extractApiError(e).message)
        setCandidates([])
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoadingBills(false)
      })
    return () => ctrl.abort()
  }, [open, partner.id, direction, tenantId, preset])

  const total = invoiceTotal(excl, tax)

  function applyInclusive(incl: string, r: string) {
    setInclusive(incl)
    const parts = splitTaxInclusive(incl, r)
    if (parts) {
      setExcl(parts.excl)
      setTax(parts.tax)
    }
  }
  function editExcl(v: string) {
    setExcl(v)
    if (rate) setInclusive(invoiceTotal(v, tax))
  }
  function editTax(v: string) {
    setTax(v)
    if (rate) setInclusive(invoiceTotal(excl, v))
  }
  const pickedIds = Object.keys(picked)
  const linkedSum = useMemo(() => sumMoney(Object.values(picked)), [picked])

  function validate(): string | null {
    if (!partner.id) return direction === "out" ? "请选择客户" : "请选择供应商"
    if (!invoiceNo.trim()) return "请填写发票号码"
    if (!issueDate) return "请选择开票日期"
    const e = parseMoney(excl)
    if (!e || e.lte(0)) return "不含税金额必须大于 0"
    if (e.decimalPlaces() > 2) return "不含税金额最多保留两位小数"
    if (tax.trim()) {
      const t = parseMoney(tax)
      if (!t || t.isNegative()) return "税额不能为负数"
      if (t.decimalPlaces() > 2) return "税额最多保留两位小数"
    }
    for (const id of pickedIds) {
      const c = candidates.find((x) => x.id === id)
      const a = parseMoney(picked[id])
      if (!a || a.lte(0)) return `请填写单据 ${c?.bill_no ?? ""} 的关联金额`
      if (a.decimalPlaces() > 2) return `单据 ${c?.bill_no ?? ""} 的关联金额最多保留两位小数`
      if (c && moneyGt(picked[id], c.remaining)) {
        return `单据 ${c.bill_no} 最多还能开票 ${formatCNY(c.remaining)}`
      }
    }
    if (moneyGt(linkedSum, total)) return "关联金额合计不能超过发票的价税合计"
    return null
  }

  const clientError = submitted ? validate() : null

  async function handleSubmit() {
    setSubmitted(true)
    setServerError(null)
    if (validate()) return
    setSaving(true)
    try {
      const inv = await createInvoice(
        {
          direction,
          invoice_no: invoiceNo.trim(),
          invoice_type: invoiceType,
          partner_id: partner.id,
          issue_date: issueDate,
          amount_excl_tax: parseMoney(excl)!.toFixed(2),
          tax_amount: (parseMoney(tax) ?? parseMoney("0")!).toFixed(2),
          remark: remark.trim() || undefined,
          links: pickedIds.map((id) => ({ bill_id: id, amount: parseMoney(picked[id])!.toFixed(2) })),
        },
        tenantId,
      )
      toast.success("发票已登记")
      onOpenChange(false)
      onCreated?.(inv)
    } catch (e) {
      setServerError(extractApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  function toggle(c: Candidate, on: boolean) {
    setPicked((p) => {
      const next = { ...p }
      if (on) next[c.id] = c.remaining
      else delete next[c.id]
      return next
    })
  }

  function changeDirection(d: InvoiceDirection) {
    if (d === direction) return
    setDirection(d)
    setPartner({ id: "", name: "" })
    setPicked({})
  }

  const partnerLabel = direction === "out" ? "客户" : "供应商"
  const Picker = direction === "out" ? CustomerPicker : SupplierPicker
  const err = clientError ?? serverError

  return (
    <Sheet
      open={open}
      onOpenChange={onOpenChange}
      title="登记发票"
      description="把开出或收到的发票记下来，并对应到具体的单据"
      className="sm:max-w-xl"
      footer={
        <div className="space-y-2">
          {err && (
            <p role="alert" className="text-sm text-destructive">
              {err}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
              取消
            </Button>
            <Button onClick={handleSubmit} disabled={saving}>
              {saving ? "保存中..." : "登记发票"}
            </Button>
          </div>
        </div>
      }
    >
      <div className="space-y-4">
        <div role="group" aria-label="发票方向" className="flex gap-2">
          {(["out", "in"] as InvoiceDirection[]).map((d) => (
            <button
              key={d}
              type="button"
              aria-pressed={direction === d}
              onClick={() => changeDirection(d)}
              className={`rounded-full border px-3 py-1 text-sm transition-colors ${
                direction === d
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-border bg-background text-muted-foreground hover:bg-muted"
              }`}
            >
              {DIRECTION_LABEL[d]}发票
            </button>
          ))}
        </div>

        <Field label={partnerLabel} required htmlFor="inv-partner">
          <Picker
            id="inv-partner"
            value={partner.id}
            label={partner.name}
            placeholder={`选择${partnerLabel}`}
            onSelect={(o) => {
              setPartner({ id: o.id, name: o.name })
              setPicked({})
            }}
            onClear={() => {
              setPartner({ id: "", name: "" })
              setPicked({})
            }}
          />
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="发票类型" required htmlFor="inv-type">
            <select
              id="inv-type"
              className={SELECT_CLASS}
              value={invoiceType}
              onChange={(e) => setInvoiceType(e.target.value)}
            >
              {INVOICE_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </Field>
          <Field label="开票日期" required htmlFor="inv-date">
            <Input id="inv-date" type="date" value={issueDate} onChange={(e) => setIssueDate(e.target.value)} />
          </Field>
        </div>

        <Field label="发票号码" required htmlFor="inv-no">
          <Input id="inv-no" value={invoiceNo} maxLength={64} onChange={(e) => setInvoiceNo(e.target.value)} />
        </Field>

        <div className="grid grid-cols-3 gap-3">
          <Field label="不含税金额" required htmlFor="inv-excl">
            <Input
              id="inv-excl"
              type="number"
              inputMode="decimal"
              min="0"
              step="0.01"
              placeholder="0.00"
              value={excl}
              onChange={(e) => editExcl(e.target.value)}
            />
          </Field>
          <Field label="税额" htmlFor="inv-tax">
            <Input
              id="inv-tax"
              type="number"
              inputMode="decimal"
              min="0"
              step="0.01"
              placeholder="0.00"
              value={tax}
              onChange={(e) => editTax(e.target.value)}
            />
          </Field>
          <Field label="价税合计" htmlFor={rate ? "inv-incl" : undefined}>
            {rate ? (
              <Input
                id="inv-incl"
                type="number"
                inputMode="decimal"
                min="0"
                step="0.01"
                placeholder="0.00"
                value={inclusive}
                onChange={(e) => applyInclusive(e.target.value, rate)}
              />
            ) : (
              <p data-testid="inv-total" className="flex h-8 items-center font-mono text-sm font-medium tabular-nums">
                {total ? formatCNY(total) : "—"}
              </p>
            )}
          </Field>
        </div>

        <Field label="税率" htmlFor="inv-rate">
          <select
            id="inv-rate"
            className={SELECT_CLASS}
            value={rate}
            onChange={(e) => {
              const r = e.target.value
              setRate(r)
              if (!r) return
              // Keep what was typed: work from the tax-inclusive total when there is one.
              applyInclusive(inclusive || total, r)
            }}
          >
            {TAX_RATES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
          {rate && <p className="mt-1 text-xs text-muted-foreground">填价税合计，不含税金额和税额会自动算出，也可以手动改</p>}
        </Field>

        <Field label="备注" htmlFor="inv-remark">
          <Input id="inv-remark" value={remark} maxLength={500} onChange={(e) => setRemark(e.target.value)} />
        </Field>

        <section aria-label="关联单据" className="space-y-2">
          <h3 className="text-sm font-medium">关联单据（可选）</h3>
          {!partner.id ? (
            <p className="text-sm text-muted-foreground">先选择{partnerLabel}，这里会列出还没开足发票的单据</p>
          ) : loadingBills ? (
            <p className="text-sm text-muted-foreground">加载中...</p>
          ) : billsError ? (
            <p className="text-sm text-destructive">{billsError}</p>
          ) : candidates.length === 0 ? (
            <p className="text-sm text-muted-foreground">该{partnerLabel}没有需要开票的已审核单据</p>
          ) : (
            <ul className="divide-y divide-border rounded-lg border border-border">
              {candidates.map((c) => {
                const on = c.id in picked
                return (
                  <li key={c.id} className="flex items-center gap-3 px-3 py-2 text-sm">
                    <input
                      type="checkbox"
                      aria-label={`关联 ${c.bill_no}`}
                      checked={on}
                      onChange={(e) => toggle(c, e.target.checked)}
                      className="size-4"
                    />
                    <div className="min-w-0 flex-1">
                      <p className="font-mono text-xs">{c.bill_no}</p>
                      <p className="text-xs text-muted-foreground">
                        {formatDate(c.bill_date)} · 合计 {formatCNY(c.total_amount)} · 未开票{" "}
                        {formatCNY(c.remaining)}
                      </p>
                    </div>
                    {on && (
                      <Input
                        type="number"
                        inputMode="decimal"
                        min="0"
                        step="0.01"
                        aria-label={`${c.bill_no} 关联金额`}
                        value={picked[c.id]}
                        onChange={(e) => setPicked((p) => ({ ...p, [c.id]: e.target.value }))}
                        className="w-28 text-right"
                      />
                    )}
                  </li>
                )
              })}
            </ul>
          )}
          {presetMissing && !loadingBills && (
            <p className="text-xs text-amber-600" role="status">
              这张单据已经开足发票或不能再开票，没有出现在上面的列表里
            </p>
          )}
          {hasMore && !loadingBills && (
            <p className="text-xs text-muted-foreground" role="status">
              待开票的单据较多，只列出了最近的 {PAGE} 张；更早的单据请先到对应的单据详情页登记发票
            </p>
          )}
          {pickedIds.length > 0 && (
            <p className="text-xs text-muted-foreground" data-testid="inv-linked">
              已关联 {pickedIds.length} 张单据，关联金额合计 {formatCNY(linkedSum)}
            </p>
          )}
        </section>
      </div>
    </Sheet>
  )
}
