"use client"

import Link from "next/link"
import { Suspense, useMemo, useState } from "react"
import { useSearchParams } from "next/navigation"
import type { ColumnDef } from "@tanstack/react-table"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Tabs } from "@/components/ui/tabs"
import { DataTable } from "@/components/ui/data-table"
import { DataCard } from "@/components/ui/data-card"
import { EmptyState } from "@/components/ui/empty-state"
import { Pagination } from "@/components/ui/pagination"
import {
  BillListFilters,
  PaymentTag,
  billRangeError,
  parseBillFilters,
  toBillFilterParams,
  type BillFilterState,
} from "@/components/bill-list-filters"
import { getReceivables, listBillsOldestFirst, type PartnerReceivable, type ReceivablesResult } from "@/lib/api/receivables"
import type { BillFilterParams, BillListSummary, PaymentStatus } from "@/lib/api/purchase"
import { formatCNY } from "@/lib/format"
import { extractApiError } from "@/lib/api/errors"
import { cn } from "@/lib/utils"
import { RoleGate } from "@/components/role-gate"

const PAGE_SIZE = 20

/** One row of either list; the sale and purchase heads share these fields. */
interface PaymentRow {
  id: string
  bill_no: string
  bill_date: string
  partner_name?: string
  total_amount: string
  paid_amount?: string
  unpaid_amount?: string
  payment_status?: PaymentStatus
}

/** The list summary; returned_amount is what sales returns take off (sales side only). */
type PaymentSummary = BillListSummary & { returned_amount?: string }

type ListParams = BillFilterParams & { page: number; size: number; signal: AbortSignal }

interface ViewConfig {
  kind: "sale" | "purchase"
  detailPath: string
  partnerLabel: string
  /** Verb used in the tags, e.g. 收 / 付. */
  verb: string
  sumLabel: string
  emptyTitle: string
  emptyDescription: string
  list: (params: ListParams) => Promise<{ items: PaymentRow[]; total: number; summary?: PaymentSummary }>
}

const SALE_VIEW: ViewConfig = {
  kind: "sale",
  detailPath: "/sales",
  partnerLabel: "客户",
  verb: "收",
  sumLabel: "未收合计",
  emptyTitle: "没有待收款的销售单",
  emptyDescription: "已审核且还没收齐的销售单会出现在这里。",
  list: (p) => listBillsOldestFirst<PaymentRow>("sale", p),
}

const PURCHASE_VIEW: ViewConfig = {
  kind: "purchase",
  detailPath: "/purchases",
  partnerLabel: "供应商",
  verb: "付",
  sumLabel: "未付合计",
  emptyTitle: "没有待付款的采购单",
  emptyDescription: "已审核且还没付清的采购单会出现在这里。",
  list: (p) => listBillsOldestFirst<PaymentRow>("purchase", p),
}

/**
 * /payments — 收付款对账.
 *
 * 应收 / 应付两个 Tab 都向后端要「未结清」的已审核单据（payment_status=open），
 * 筛选和分页由服务端完成，合计取服务端 summary，不在浏览器里累加。
 * 单据按单据日期从旧到新排，欠得最久的排在最前；顶部的汇总按客户 / 供应商
 * 统计欠款和账龄，点“查看单据”只看这一家。统计口径一律只计已审核的单据。
 */
export default function PaymentsPage() {
  return (
    <RoleGate perm="payment.read" title="付款 / 对账" subtitle="应收和应付的单据" notice="付款和对账只对老板、店长和财务开放" waitForRole>
      <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">加载中...</div>}>
        <PaymentsPageInner />
      </Suspense>
    </RoleGate>
  )
}

const AS_OF_RE = /^\d{4}-\d{2}-\d{2}$/

function PaymentsPageInner() {
  const sp = useSearchParams()
  const [tab, setTabState] = useState<"sales" | "purchases">(sp.get("tab") === "purchases" ? "purchases" : "sales")
  // The partner in the link belongs to the tab the link opened; once the user switches
  // tabs it must not leak into the other side (a customer id filtering purchases).
  const [switched, setSwitched] = useState(false)
  const setTab = (v: "sales" | "purchases") => {
    if (v !== tab) setSwitched(true)
    setTabState(v)
  }
  const params = switched ? new URLSearchParams() : sp
  // 月底对账页带来的截止日：账龄汇总按这一天算，和报表卡片对得上
  const rawAsOf = sp.get("as_of")
  const asOf = rawAsOf && AS_OF_RE.test(rawAsOf) ? rawAsOf : undefined

  return (
    <PageContainer width="default">
      <PageHeader
        title="付款 / 对账"
        subtitle="还没收齐的销售单（应收）和还没付清的采购单（应付）。点击详情登记收款或付款。"
      />

      {asOf && (
        <p className="mb-3 text-sm text-muted-foreground">
          上方汇总是 {asOf} 及以前开的单，扣掉到今天为止的收付款后还欠的；下方单据列表是当前还没结清的单据。{" "}
          <Link href={`/payments?tab=${tab}`} className="text-primary hover:underline">
            看今天
          </Link>
        </p>
      )}

      <Tabs
        variant="segment"
        className="mb-4"
        value={tab}
        onValueChange={setTab}
        items={[
          { label: "应收（销售）", value: "sales" },
          { label: "应付（采购）", value: "purchases" },
        ]}
      />

      {tab === "sales" ? (
        <OpenBillsView key="sale" cfg={SALE_VIEW} direction="ar" initialParams={params} asOf={asOf} />
      ) : (
        <OpenBillsView key="purchase" cfg={PURCHASE_VIEW} direction="ap" initialParams={params} asOf={asOf} />
      )}
    </PageContainer>
  )
}

// Default is "all time" so the oldest debts show up. The settlement dropdown
// starts on "open" (not settled); picking 全部 there sends no payment_status.
// A link from the customer / supplier page carries the partner (partner + pname).
function initialFilters(sp: URLSearchParams): BillFilterState {
  const base = parseBillFilters(new URLSearchParams())
  return {
    ...base,
    range: "all",
    payment: "open",
    partnerId: sp.get("partner") ?? "",
    partnerName: sp.get("pname") ?? "",
  }
}

/** Amounts arrive as decimal strings; zero buckets render as a dash. */
function bucketCell(v: string, danger = false) {
  const n = Number(v)
  if (!(n > 0)) return <span className="block text-right text-muted-foreground">—</span>
  return (
    <span className={cn("block text-right font-mono tabular-nums", danger && "text-destructive")}>{formatCNY(v)}</span>
  )
}

const TOP_PARTNERS = 8

/** 按客户 / 供应商汇总欠款和账龄（按单据日期分档，只计已审核）。 */
function AgingPanel({
  direction,
  label,
  verb,
  asOf,
  onPick,
}: {
  direction: "ar" | "ap"
  label: string
  verb: string
  asOf?: string
  onPick: (id: string, name: string) => void
}) {
  const [data, setData] = useState<ReceivablesResult | null>(null)
  const [failed, setFailed] = useState(false)
  const [showAll, setShowAll] = useState(false)

  useAbortableEffect(
    (signal, isCancelled) => {
      getReceivables(direction, { signal, asOf })
        .then((r) => {
          if (!isCancelled()) setData(r)
        })
        .catch(() => {
          if (!isCancelled() && !signal.aborted) setFailed(true)
        })
    },
    [direction, asOf]
  )

  if (failed) {
    return <p className="mb-4 text-xs text-muted-foreground">按{label}汇总暂时加载失败，下面的单据列表不受影响。</p>
  }
  if (!data || data.items.length === 0) return null

  const rows: PartnerReceivable[] = showAll ? data.items : data.items.slice(0, TOP_PARTNERS)
  const head = "px-3 py-2 text-right font-medium"
  return (
    <section className="mb-5 rounded-lg border bg-card" data-testid="aging-panel">
      <div className="flex items-baseline justify-between px-3 pt-3">
        <h2 className="text-sm font-medium">按{label}汇总</h2>
        <span className="text-xs text-muted-foreground">{asOf ? `截至 ${asOf} · ` : ""}只计已审核单据 · 按单据日期算天数</span>
      </div>
      <div className="overflow-x-auto">
        <table className="mt-2 w-full text-sm">
          <thead className="text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2 text-left font-medium">{label}</th>
              <th className={head}>未{verb}合计</th>
              <th className={head}>30 天内</th>
              <th className={head}>31–60 天</th>
              <th className={head}>61–90 天</th>
              <th className={head}>90 天以上</th>
              <th className={head}>最早一笔</th>
              <th className={head}>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.partner_id ?? "none"} className="border-t">
                <td className="px-3 py-2">{r.partner_name}</td>
                <td className="px-3 py-2 text-right font-mono font-medium tabular-nums text-warning">
                  {formatCNY(r.unpaid)}
                </td>
                <td className="px-3 py-2">{bucketCell(r.b0_30)}</td>
                <td className="px-3 py-2">{bucketCell(r.b31_60)}</td>
                <td className="px-3 py-2">{bucketCell(r.b61_90)}</td>
                <td className="px-3 py-2">{bucketCell(r.b90p, true)}</td>
                <td className="px-3 py-2 text-right text-xs text-muted-foreground">{r.oldest_bill_date || "—"}</td>
                <td className="px-3 py-2 text-right">
                  {r.partner_id ? (
                    <button
                      type="button"
                      className="text-xs text-primary hover:underline"
                      onClick={() => onPick(r.partner_id as string, r.partner_name)}
                    >
                      查看单据
                    </button>
                  ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                  )}
                </td>
              </tr>
            ))}
            <tr className="border-t bg-muted/40 font-medium">
              <td className="px-3 py-2">合计（共 {data.items.length} 家）</td>
              <td className="px-3 py-2 text-right font-mono tabular-nums text-warning">{formatCNY(data.total.unpaid)}</td>
              <td className="px-3 py-2">{bucketCell(data.total.b0_30)}</td>
              <td className="px-3 py-2">{bucketCell(data.total.b31_60)}</td>
              <td className="px-3 py-2">{bucketCell(data.total.b61_90)}</td>
              <td className="px-3 py-2">{bucketCell(data.total.b90p, true)}</td>
              <td className="px-3 py-2" colSpan={2} />
            </tr>
          </tbody>
        </table>
      </div>
      {data.items.length > TOP_PARTNERS && (
        <button
          type="button"
          className="w-full border-t px-3 py-2 text-xs text-primary hover:underline"
          onClick={() => setShowAll((v) => !v)}
        >
          {showAll ? "收起" : `显示全部 ${data.items.length} 家`}
        </button>
      )}
    </section>
  )
}

function OpenBillsView({
  cfg,
  direction,
  initialParams,
  asOf,
}: {
  cfg: ViewConfig
  direction: "ar" | "ap"
  initialParams: URLSearchParams
  asOf?: string
}) {
  const [filters, setFilters] = useState<BillFilterState>(() => initialFilters(initialParams))
  const [page, setPage] = useState(1)
  const [rows, setRows] = useState<PaymentRow[]>([])
  const [total, setTotal] = useState(0)
  const [summary, setSummary] = useState<PaymentSummary | undefined>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const returned = Number(summary?.returned_amount ?? 0)
  const apiFilters = useMemo(() => toBillFilterParams(filters), [filters])

  useAbortableEffect(
    (signal, isCancelled) => {
      const rangeErr = billRangeError(filters)
      if (rangeErr) {
        setRows([])
        setTotal(0)
        setSummary(undefined)
        setError(rangeErr)
        setLoading(false)
        return
      }
      setLoading(true)
      setError(null)
      cfg
        .list({ ...apiFilters, status: 2, page, size: PAGE_SIZE, signal })
        .then((r) => {
          if (isCancelled()) return
          setRows(r.items ?? [])
          setTotal(r.total ?? 0)
          setSummary(r.summary)
        })
        .catch((err: unknown) => {
          if (isCancelled() || signal.aborted) return
          setError(extractApiError(err).message)
        })
        .finally(() => {
          if (isCancelled()) return
          setLoading(false)
        })
    },
    [cfg, apiFilters, filters, page]
  )

  const onFilterChange = (patch: Partial<BillFilterState>) => {
    setFilters((f) => ({ ...f, ...patch }))
    setPage(1)
  }

  const columns: ColumnDef<PaymentRow>[] = [
    {
      id: "bill_no",
      header: "单号",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.bill_no}</span>,
    },
    {
      id: "date",
      header: "单据日期",
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">{row.original.bill_date?.slice(0, 10) ?? "—"}</span>
      ),
    },
    {
      id: "partner",
      header: cfg.partnerLabel,
      cell: ({ row }) => <span className="text-sm">{row.original.partner_name || "—"}</span>,
    },
    {
      id: "total",
      header: "合计",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="block text-right font-mono tabular-nums">{formatCNY(row.original.total_amount)}</span>
      ),
    },
    {
      id: "paid",
      header: `已${cfg.verb}`,
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="block text-right font-mono tabular-nums text-success">
          {formatCNY(row.original.paid_amount)}
        </span>
      ),
    },
    {
      id: "unpaid",
      header: `未${cfg.verb}`,
      meta: { align: "right" },
      cell: ({ row }) => {
        const owed = Number(row.original.unpaid_amount) > 0
        return (
          <span className={cn("block text-right font-mono tabular-nums", owed ? "text-warning" : "text-muted-foreground")}>
            {formatCNY(row.original.unpaid_amount)}
          </span>
        )
      },
    },
    {
      id: "status",
      header: "状态",
      cell: ({ row }) => <PaymentTag kind={cfg.kind} status={row.original.payment_status} />,
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <Link href={`${cfg.detailPath}/${row.original.id}`} className="text-xs text-primary hover:underline">
          详情
        </Link>
      ),
    },
  ]

  return (
    <>
      <AgingPanel
        direction={direction}
        label={cfg.partnerLabel}
        verb={cfg.verb}
        asOf={asOf}
        onPick={(id, name) => onFilterChange({ partnerId: id, partnerName: name })}
      />

      <BillListFilters kind={cfg.kind} filters={filters} onChange={onFilterChange} />

      {summary && !error && (
        <p className="mb-3 text-sm text-muted-foreground">
          合计{" "}
          <span className="font-mono tabular-nums">{formatCNY(summary.total_amount)}</span>
          {returned > 0 && (
            <>
              {" − 退货冲减 "}
              <span className="font-mono tabular-nums">{formatCNY(summary?.returned_amount)}</span>
            </>
          )}
          {" − 已"}
          {cfg.verb}{" "}
          <span className="font-mono tabular-nums">{formatCNY(summary.paid_amount)}</span>
          {" = "}
          {cfg.sumLabel}{" "}
          <span className="font-mono text-base font-medium tabular-nums text-warning">
            {formatCNY(summary.unpaid_amount)}
          </span>
          <span className="ml-2 text-xs">共 {summary.count} 张 · 只计已审核单据</span>
        </p>
      )}

      <DataTable
        columns={columns}
        data={rows}
        loading={loading}
        error={error}
        getRowId={(b) => b.id}
        skeletonRows={8}
        mobileCard={(b) => (
          <DataCard
            title={<span className="font-mono text-xs">{b.bill_no}</span>}
            subtitle={b.partner_name || "—"}
            aside={<PaymentTag kind={cfg.kind} status={b.payment_status} />}
            fields={[
              { label: "合计", value: formatCNY(b.total_amount) },
              { label: `已${cfg.verb}`, value: formatCNY(b.paid_amount) },
              {
                label: `未${cfg.verb}`,
                value: (
                  <span className={cn(Number(b.unpaid_amount) > 0 ? "text-warning" : "text-muted-foreground")}>
                    {formatCNY(b.unpaid_amount)}
                  </span>
                ),
              },
              { label: "单据日期", value: b.bill_date?.slice(0, 10) ?? "—" },
            ]}
            actions={
              <Link href={`${cfg.detailPath}/${b.id}`} className="text-xs text-primary hover:underline">
                详情
              </Link>
            }
          />
        )}
        animateRows
        empty={<EmptyState title={cfg.emptyTitle} description={cfg.emptyDescription} />}
      />

      <Pagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} onPageChange={setPage} />
    </>
  )
}
