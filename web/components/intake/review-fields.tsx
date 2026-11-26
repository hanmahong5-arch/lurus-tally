"use client"

import { useEffect, useRef } from "react"
import { EntityCombobox } from "@/components/entity-combobox"
import { Input } from "@/components/ui/input"
import { apiFetch } from "@/lib/api/client"
import type { IntakeDoc } from "@/lib/api/intake"
import { coverNote, suggestBillIds } from "@/lib/intake/match"
import { moneyGap, moneyReconciles } from "@/lib/intake/money"
import { TAX_RATES, splitTaxInclusive } from "@/lib/invoice-tax"

const TYPES = ["数电普票", "数电专票", "纸质普票", "纸质专票"] as const

export interface ReviewForm {
  direction: string
  invoiceNo: string
  invoiceCode: string
  invoiceType: string
  issueDate: string
  excl: string
  tax: string
  total: string
  partnerId: string
  partnerName: string
  links: string[]
  matchId: string
  draft: boolean
  prices: Record<string, string>
  forId: string
}

export const emptyReview: ReviewForm = {
  direction: "in", invoiceNo: "", invoiceCode: "", invoiceType: "", issueDate: "",
  excl: "", tax: "", total: "", partnerId: "", partnerName: "", links: [], matchId: "", draft: false, prices: {}, forId: "",
}

export function formFromDoc(doc: IntakeDoc): ReviewForm {
  const p = doc.parsed ?? {}
  return {
    direction: doc.direction || "in",
    invoiceNo: p.invoice_no || doc.invoice_no || "",
    invoiceCode: p.invoice_code || "",
    invoiceType: p.invoice_type || "",
    issueDate: (p.issue_date || doc.issue_date || "").slice(0, 10),
    excl: p.amount_excl_tax || "",
    tax: p.tax_amount || "",
    total: p.total_amount || "",
    partnerId: p.partner_id || "",
    partnerName: p.partner_name || "",
    links: [],
    matchId: p.matches?.[0]?.bill_id || "",
    draft: false,
    prices: Object.fromEntries((p.lines ?? []).map((ln, i) => [String(i), ln.unit_price || ""])),
    forId: doc.id,
  }
}

function knownType(value: string): boolean {
  return (TYPES as readonly string[]).includes(value)
}

/** Confirm stays off until the three amounts agree and the invoice fields are the ones the registry accepts. */
export function reviewReady(form: ReviewForm, source: string, locked: boolean): boolean {
  if (locked) return false
  if (source === "receipt" && form.matchId) return true
  return moneyReconciles(form.excl, form.tax, form.total)
    && form.invoiceNo.trim() !== ""
    && knownType(form.invoiceType)
    && form.issueDate !== ""
    && form.partnerId !== ""
}

type Bill = NonNullable<IntakeDoc["open_bills"]>[number]

export function ReviewFields({ doc, form, bills, onChange }: {
  doc: IntakeDoc
  form: ReviewForm
  bills: Bill[]
  onChange: (patch: Partial<ReviewForm>) => void
}) {
  const receipt = doc.source === "receipt"
  const reconciled = moneyReconciles(form.excl, form.tax, form.total)
  const ready = reviewReady(form, doc.source, false)
  const picked = bills.filter((b) => form.links.includes(b.id))
  const note = coverNote(form.total, picked.map((b) => b.remaining || ""))
  const suggested = useRef("")
  useEffect(() => {
    const key = `${doc.id}|${form.total}|${bills.map((b) => b.id).join(",")}`
    if (suggested.current === key || form.links.length > 0) return
    suggested.current = key
    const ids = suggestBillIds(form.total, bills)
    if (ids.length) onChange({ links: ids })
  }, [doc.id, form.total, form.links.length, bills, onChange])
  const types = knownType(form.invoiceType) || form.invoiceType === "" ? TYPES : [form.invoiceType, ...TYPES]
  const search = async (q: string, signal: AbortSignal) => {
    const path = form.direction === "out" ? "/customers" : "/suppliers"
    const res = await apiFetch<{ items?: { id: string; name: string }[] }>(`${path}?limit=20&q=${encodeURIComponent(q)}`, { silent: true, signal })
    return (res.items ?? []).map((p) => ({ id: p.id, name: p.name }))
  }
  return (
    <div className="space-y-2 text-sm">
      {(doc.parsed.hints ?? []).map((h) => <p key={h}>{h}</p>)}
      {doc.parsed.seller_name && <p>销方：{doc.parsed.seller_name}</p>}
      {doc.parsed.seller_tax_no && <p>税号：{doc.parsed.seller_tax_no}</p>}
      {doc.parsed.transcript && <p>语音：{doc.parsed.transcript}</p>}
      {!receipt && !reconciled && <p>金额待填。不含税、税额、价税合计对不齐时不能入账。</p>}
      {reconciled && !ready && !receipt && <p>请补全发票号码、发票类型、往来单位和开票日期。</p>}
      <label className="block">方向
        <select className="ml-2 rounded border px-2 py-1" aria-label="方向" value={form.direction} onChange={(e) => onChange({ direction: e.target.value, partnerId: "", partnerName: "", links: [] })}>
          <option value="in">进项</option>
          <option value="out">销项</option>
        </select>
      </label>
      <Field label="发票号码" value={form.invoiceNo} onChange={(invoiceNo) => onChange({ invoiceNo })} />
      <Field label="发票代码" value={form.invoiceCode} onChange={(invoiceCode) => onChange({ invoiceCode })} />
      <label className="block">发票类型
        <select className="mt-1 w-full rounded border px-2 py-1" aria-label="发票类型" value={form.invoiceType} onChange={(e) => onChange({ invoiceType: e.target.value })}>
          <option value="">请选择</option>
          {types.map((t) => <option key={t} value={t}>{t}</option>)}
        </select>
      </label>
      <label className="block">开票日期
        <Input className="mt-1" type="date" aria-label="开票日期" value={form.issueDate} onChange={(e) => onChange({ issueDate: e.target.value })} />
      </label>
      <Field label="不含税金额" value={form.excl} onChange={(excl) => onChange({ excl })} />
      <Field label="税额" value={form.tax} onChange={(tax) => onChange({ tax })} />
      <Field label="价税合计" value={form.total} onChange={(total) => onChange({ total })} />
      <label className="block">税率
        <select key={form.total} aria-label="税率" className="ml-2 rounded border px-2 py-1" defaultValue="" onChange={(e) => {
          const parts = splitTaxInclusive(form.total, e.target.value)
          if (parts) onChange({ excl: parts.excl, tax: parts.tax })
        }}>
          {TAX_RATES.map((t) => <option key={t.label} value={t.value}>{t.label}</option>)}
        </select>
      </label>
      <div>
        <p className="mb-1">往来单位</p>
        <EntityCombobox
          value={form.partnerId}
          label={form.partnerName}
          entityLabel={form.direction === "out" ? "客户" : "供应商"}
          placeholder={form.direction === "out" ? "搜索客户" : "搜索供应商"}
          search={search}
          onSelect={(p) => onChange({ partnerId: p.id, partnerName: p.name, links: [] })}
          onClear={() => onChange({ partnerId: "", partnerName: "", links: [] })}
        />
      </div>
      {bills.length > 0 && <button type="button" className="underline" onClick={() => onChange({ links: suggestBillIds(form.total, bills) })}>按未开票金额凑这张票</button>}
      {bills.map((b) => (
        <label key={b.id} className="block">
          <input type="checkbox" disabled={b.status !== 2} checked={form.links.includes(b.id)} onChange={(e) => onChange({ links: e.target.checked ? [...form.links, b.id] : form.links.filter((id) => id !== b.id) })} />
          {" "}{b.bill_no}{b.bill_date ? ` ${b.bill_date}` : ""} 未开票 {b.remaining || ""} {b.remaining ? moneyGap(form.total, b.remaining) : ""} {b.status === 2 ? "" : "未审核，先去审核这张单"}
        </label>
      ))}
      {note && <p>{note}</p>}
      {(doc.parsed.matches ?? []).map((m) => (
        <label key={m.bill_id} className="block">
          <input type="radio" name="match" checked={form.matchId === m.bill_id} onChange={() => onChange({ matchId: m.bill_id })} />
          {" "}回单对照 {m.bill_no} {m.amount ?? ""}，入账不会自动收款
        </label>
      ))}
      {(doc.parsed.lines ?? []).map((ln, i) => (
        <div key={`${ln.name}-${i}`} className="flex items-center gap-2">
          <span>{ln.name || "未命名"} {ln.qty ? `数量 ${ln.qty}` : ""} {ln.product_id ? "" : "商品未匹配，不会自动建档"}</span>
          <Input aria-label={`${ln.name || "行"}单价`} value={form.prices[String(i)] || ""} onChange={(e) => onChange({ prices: { ...form.prices, [String(i)]: e.target.value } })} />
        </div>
      ))}
      {doc.source !== "receipt" && <label className="block"><input type="checkbox" checked={form.draft} onChange={(e) => onChange({ draft: e.target.checked })} /> 需要时生成采购草稿，不审核</label>}
    </div>
  )
}

function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return (
    <label className="block">{label}
      <Input className="mt-1" aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} />
    </label>
  )
}
