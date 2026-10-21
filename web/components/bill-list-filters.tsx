"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { EntityCombobox, type EntityOption } from "@/components/entity-combobox"
import { Input } from "@/components/ui/input"
import { useTenantId } from "@/hooks/use-tenant-id"
import { getCustomer, listCustomers } from "@/lib/api/customers"
import { getSupplier, listSuppliers } from "@/lib/api/suppliers"
import type {
  BillFilterParams,
  BillListSummary,
  BillStatus,
  InvoiceStatus,
  PaymentFilter,
  PaymentStatus,
} from "@/lib/api/purchase"
import { Badge, type BadgeTone } from "@/components/ui/badge"
import { formatCNY } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  RANGE_PRESETS,
  RANGE_PRESET_LABEL,
  resolveDateRange,
  type RangePreset,
} from "@/lib/date-range"

export type BillListKind = "sale" | "purchase"

export interface BillFilterState {
  range: RangePreset
  /** Custom range bounds (YYYY-MM-DD); only used when range === "custom". */
  from: string
  to: string
  partnerId: string
  partnerName: string
  payment: PaymentFilter | ""
  /** Invoice coverage (approved bills only); "" = any. */
  invoice: InvoiceStatus | ""
  q: string
  mine: boolean
  /** Status tab (draft / approved / cancelled); undefined = all. */
  status: BillStatus | undefined
}

export const DEFAULT_RANGE: RangePreset = "this_month"

const PAYMENT_VALUES: PaymentFilter[] = ["open", "unpaid", "partial", "paid"]
const INVOICE_VALUES: InvoiceStatus[] = ["none", "partial", "full"]

type QueryReader = { get(name: string): string | null }

/** URL query -> filter state. Unknown / malformed values fall back to defaults. */
export function parseBillFilters(sp: QueryReader): BillFilterState {
  const range = sp.get("range")
  const pay = sp.get("pay")
  const inv = sp.get("inv")
  const st = sp.get("status")
  return {
    range: RANGE_PRESETS.includes(range as RangePreset) ? (range as RangePreset) : DEFAULT_RANGE,
    from: sp.get("from") ?? "",
    to: sp.get("to") ?? "",
    partnerId: sp.get("partner") ?? "",
    partnerName: sp.get("pname") ?? "",
    payment: PAYMENT_VALUES.includes(pay as PaymentFilter) ? (pay as PaymentFilter) : "",
    invoice: INVOICE_VALUES.includes(inv as InvoiceStatus) ? (inv as InvoiceStatus) : "",
    q: sp.get("q") ?? "",
    mine: sp.get("mine") === "1",
    status: st === "0" || st === "2" || st === "9" ? (Number(st) as BillStatus) : undefined,
  }
}

const OWN_KEYS = ["range", "from", "to", "partner", "pname", "pay", "inv", "q", "mine", "status"]

/** Filter state -> URL query; defaults are omitted so the plain list URL stays clean. */
export function writeBillFilters(base: URLSearchParams, f: BillFilterState): URLSearchParams {
  const usp = new URLSearchParams(base.toString())
  OWN_KEYS.forEach((k) => usp.delete(k))
  if (f.range !== DEFAULT_RANGE) usp.set("range", f.range)
  if (f.range === "custom") {
    if (f.from) usp.set("from", f.from)
    if (f.to) usp.set("to", f.to)
  }
  if (f.partnerId) {
    usp.set("partner", f.partnerId)
    if (f.partnerName) usp.set("pname", f.partnerName)
  }
  if (f.payment) usp.set("pay", f.payment)
  if (f.invoice) usp.set("inv", f.invoice)
  if (f.q) usp.set("q", f.q)
  if (f.mine) usp.set("mine", "1")
  if (f.status !== undefined) usp.set("status", String(f.status))
  return usp
}

/** Filter state -> list API params (dates resolved on the Asia/Shanghai calendar). */
export function toBillFilterParams(f: BillFilterState, now: Date = new Date()): BillFilterParams {
  const { from, to } = resolveDateRange(f.range, now, { from: f.from, to: f.to })
  return {
    date_from: from || undefined,
    date_to: to || undefined,
    partner_id: f.partnerId || undefined,
    payment_status: f.payment || undefined,
    invoice_status: f.invoice || undefined,
    q: f.q || undefined,
    mine: f.mine || undefined,
  }
}

/** Message when a custom range starts after it ends (typed by hand); "" when fine. */
export function billRangeError(f: BillFilterState): string {
  if (f.range === "custom" && f.from && f.to && f.from > f.to) return "开始日期不能晚于结束日期"
  return ""
}

/** True when anything beyond the default range is narrowing the list. */
export function hasActiveBillFilters(f: BillFilterState): boolean {
  return f.range !== DEFAULT_RANGE || !!f.partnerId || !!f.payment || !!f.invoice || !!f.q || f.mine || f.status !== undefined
}

/**
 * Filter state kept in the URL (router.replace) so a filtered list can be
 * shared and the back button restores it.
 */
export function useBillFilters() {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const sp = searchParams?.toString() ?? ""
  const filters = useMemo(() => parseBillFilters(new URLSearchParams(sp)), [sp])

  const replace = useCallback(
    (next: BillFilterState) => {
      const qs = writeBillFilters(new URLSearchParams(sp), next).toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [router, pathname, sp],
  )

  const update = useCallback(
    (patch: Partial<BillFilterState>) => replace({ ...parseBillFilters(new URLSearchParams(sp)), ...patch }),
    [replace, sp],
  )

  const reset = useCallback(() => replace(parseBillFilters(new URLSearchParams())), [replace])

  return { filters, update, reset }
}

const INVOICE_OPTIONS: { value: InvoiceStatus | ""; label: string }[] = [
  { value: "", label: "全部开票状态" },
  { value: "none", label: "未开票" },
  { value: "partial", label: "部分开票" },
  { value: "full", label: "已开票" },
]

const PAYMENT_OPTIONS: { value: PaymentFilter | ""; label: (verb: string) => string }[] = [
  { value: "", label: () => "全部" },
  { value: "open", label: () => "未结清" },
  { value: "unpaid", label: (verb) => `未${verb}` },
  { value: "partial", label: () => "部分" },
  { value: "paid", label: () => "已结清" },
]

interface Props {
  kind: BillListKind
  filters: BillFilterState
  onChange: (patch: Partial<BillFilterState>) => void
}

export function BillListFilters({ kind, filters, onChange }: Props) {
  const verb = kind === "sale" ? "收" : "付"
  const partnerLabel = kind === "sale" ? "客户" : "供应商"
  const tenantId = useTenantId()

  // Select-only: filtering never creates a customer / supplier.
  const searchPartner = useCallback(
    async (q: string, signal: AbortSignal): Promise<EntityOption[]> => {
      if (kind === "sale") {
        const res = await listCustomers({ q: q || undefined, limit: 20, tenantId, signal })
        return res.items.map((c) => ({ id: c.id, name: c.name, sub: c.mobile || c.phone || c.code || undefined }))
      }
      const res = await listSuppliers({ q: q || undefined, limit: 20, tenantId, signal })
      return res.items.map((s) => ({ id: s.id, name: s.name, sub: s.phone || s.code || undefined }))
    },
    [kind, tenantId],
  )

  // A link may carry the partner id without its name: look the name up so the
  // box never claims "all" while the list is filtered.
  const [fetched, setFetched] = useState<{ id: string; name: string } | null>(null)
  useEffect(() => {
    if (!filters.partnerId || filters.partnerName) return
    let live = true
    const id = filters.partnerId
    const get = kind === "sale" ? getCustomer(id, tenantId) : getSupplier(id, tenantId)
    get
      .then((p) => live && setFetched({ id, name: p.name }))
      .catch(() => live && setFetched({ id, name: "已选 1 个" + partnerLabel }))
    return () => {
      live = false
    }
  }, [filters.partnerId, filters.partnerName, kind, tenantId, partnerLabel])
  const partnerShown =
    filters.partnerName || (fetched && fetched.id === filters.partnerId ? fetched.name : "")

  // Search box: type freely, push to the URL 300ms after the last keystroke.
  const [qText, setQText] = useState(filters.q)
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange
  // Last value pushed to the URL: the URL echoing it back must not overwrite
  // keystrokes typed meanwhile; only a different value (back, clear) syncs in.
  const lastPushed = useRef(filters.q)
  useEffect(() => {
    if (filters.q !== lastPushed.current) {
      lastPushed.current = filters.q
      setQText(filters.q)
    }
  }, [filters.q])
  useEffect(() => {
    const next = qText.trim()
    if (next === filters.q) return
    const t = setTimeout(() => {
      lastPushed.current = next
      onChangeRef.current({ q: next })
    }, 300)
    return () => clearTimeout(t)
  }, [qText, filters.q])

  return (
    <div className="mb-4 space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div role="group" aria-label="时间范围" className="flex flex-wrap gap-1.5">
          {RANGE_PRESETS.map((p) => (
            <button
              key={p}
              type="button"
              aria-pressed={filters.range === p}
              onClick={() =>
                // Switching to custom starts from the range on screen, never an empty (= all time) one.
                onChange(
                  p === "custom" && filters.range !== "custom" && !filters.from && !filters.to
                    ? { range: p, ...resolveDateRange(filters.range === "all" ? DEFAULT_RANGE : filters.range) }
                    : { range: p },
                )
              }
              className={cn(
                "rounded-full border px-3 py-1 text-xs transition-colors",
                filters.range === p
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-border bg-background text-muted-foreground hover:bg-muted",
              )}
            >
              {RANGE_PRESET_LABEL[p]}
            </button>
          ))}
        </div>
        {filters.range === "custom" && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Input
              type="date"
              aria-label="开始日期"
              value={filters.from}
              max={filters.to || undefined}
              onChange={(e) => onChange({ from: e.target.value })}
              className="w-36"
            />
            <span>至</span>
            <Input
              type="date"
              aria-label="结束日期"
              value={filters.to}
              min={filters.from || undefined}
              onChange={(e) => onChange({ to: e.target.value })}
              className="w-36"
            />
          </div>
        )}
        {billRangeError(filters) && (
          <span role="alert" className="text-xs text-destructive">
            {billRangeError(filters)}
          </span>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <EntityCombobox
          id="bill-filter-partner"
          value={filters.partnerId}
          label={partnerShown}
          search={searchPartner}
          entityLabel={partnerLabel}
          placeholder={`全部${partnerLabel}`}
          onSelect={(o) => onChange({ partnerId: o.id, partnerName: o.name })}
          onClear={() => onChange({ partnerId: "", partnerName: "" })}
          className="w-44"
        />
        <select
          aria-label={`${verb === "收" ? "收款" : "付款"}状态`}
          value={filters.payment}
          onChange={(e) => onChange({ payment: e.target.value as PaymentFilter | "" })}
          className="h-8 rounded-lg border border-input bg-background px-2 text-sm"
        >
          {PAYMENT_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label(verb)}
            </option>
          ))}
        </select>
        <select
          aria-label="开票状态"
          value={filters.invoice}
          onChange={(e) => onChange({ invoice: e.target.value as InvoiceStatus | "" })}
          className="h-8 rounded-lg border border-input bg-background px-2 text-sm"
        >
          {INVOICE_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <Input
          type="search"
          aria-label="搜索"
          placeholder={`搜单号或${partnerLabel}名称`}
          value={qText}
          onChange={(e) => setQText(e.target.value)}
          className="w-56"
        />
        <button
          type="button"
          role="switch"
          aria-checked={filters.mine}
          onClick={() => onChange({ mine: !filters.mine })}
          className="flex items-center gap-2 text-sm"
        >
          <span
            aria-hidden="true"
            className={cn(
              "relative inline-block h-5 w-9 rounded-full transition-colors",
              filters.mine ? "bg-primary" : "bg-muted-foreground/30",
            )}
          >
            <span
              className={cn(
                "absolute top-0.5 h-4 w-4 rounded-full bg-background transition-all",
                filters.mine ? "left-[18px]" : "left-0.5",
              )}
            />
          </span>
          只看我的
        </button>
      </div>
    </div>
  )
}

const PAYMENT_TAG: Record<PaymentStatus, { label: (verb: string) => string; tone: BadgeTone }> = {
  unpaid: { label: (verb) => `未${verb}`, tone: "err" },
  partial: { label: () => "部分", tone: "warn" },
  paid: { label: () => "已结清", tone: "ok" },
}

/** Small settlement tag for a list row; renders nothing when the backend sent no status. */
export function PaymentTag({ status, kind }: { status?: PaymentStatus; kind: BillListKind }) {
  if (!status || !PAYMENT_TAG[status]) return null
  const t = PAYMENT_TAG[status]
  return <Badge tone={t.tone}>{t.label(kind === "sale" ? "收" : "付")}</Badge>
}

const INVOICE_TAG: Record<InvoiceStatus, { label: string; tone: BadgeTone }> = {
  none: { label: "未开票", tone: "err" },
  partial: { label: "部分开票", tone: "warn" },
  full: { label: "已开票", tone: "ok" },
}

/** Invoice-coverage tag for a list row; only approved bills carry one. */
export function InvoiceTag({ status, billStatus }: { status?: InvoiceStatus; billStatus: BillStatus }) {
  if (billStatus !== 2) return <span className="text-muted-foreground">—</span>
  if (!status || !INVOICE_TAG[status]) return null
  const t = INVOICE_TAG[status]
  return <Badge tone={t.tone}>{t.label}</Badge>
}

/**
 * 「共 N 单 · 合计 ¥X · 已收 ¥Y · 未收 ¥Z」. Amounts come from the backend's
 * summary over the whole filtered set (not just this page) and are formatted
 * one by one — never added up in the browser.
 */
export function BillSummaryBar({
  kind,
  summary,
  total,
}: {
  kind: BillListKind
  summary?: BillListSummary
  total: number
}) {
  const verb = kind === "sale" ? "收" : "付"
  return (
    <p className="mb-3 text-sm text-muted-foreground" data-testid="bill-summary">
      {summary ? (
        <>
          共 {summary.count} 单 · 合计 {formatCNY(summary.total_amount)} · 已{verb}{" "}
          {formatCNY(summary.paid_amount)} · 未{verb}{" "}
          <span className="font-medium text-warning">{formatCNY(summary.unpaid_amount)}</span>
        </>
      ) : (
        <>共 {total} 单</>
      )}
    </p>
  )
}
