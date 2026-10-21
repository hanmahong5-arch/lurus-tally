"use client"

import { Suspense, useCallback, useMemo, useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  listSaleBills,
  cancelSaleBill,
  restoreSaleBill,
  billsExportUrl,
  type SaleBillHead,
  type BillStatus,
  type BillListSummary,
} from "@/lib/api/sale"
import { BILL_STATUS_TONE, BILL_STATUS_LABEL } from "@/lib/status"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import {
  BillListFilters,
  BillSummaryBar,
  PaymentTag,
  InvoiceTag,
  hasActiveBillFilters,
  toBillFilterParams,
  billRangeError,
  useBillFilters,
  type BillFilterState,
} from "@/components/bill-list-filters"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable, currencyCell, statusCell } from "@/components/ui/data-table"
import { DataCard } from "@/components/ui/data-card"
import { Tabs, type TabItem } from "@/components/ui/tabs"
import { Pagination } from "@/components/ui/pagination"
import { buttonVariants } from "@/components/ui/button"
import { EmptyState } from "@/components/ui/empty-state"
import { formatCNY, formatDate } from "@/lib/format"
import { extractApiError } from "@/lib/api/errors"

const PAGE_SIZE = 20

const STATUS_TABS: TabItem<BillStatus | undefined>[] = [
  { label: "全部", value: undefined },
  { label: "草稿", value: 0 },
  { label: "已审核", value: 2 },
  { label: "已取消", value: 9 },
]

function SalesPageInner() {
  const router = useRouter()
  const { filters, update, reset } = useBillFilters()
  const status = filters.status
  const [bills, setBills] = useState<SaleBillHead[]>([])
  const [total, setTotal] = useState(0)
  const [summary, setSummary] = useState<BillListSummary | undefined>(undefined)
  const [page, setPage] = useState(1)
  const [reloadTick, setReloadTick] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const tenantId = useTenantId()
  const { known: roleKnown, can } = useRole()
  const mayDraft = !roleKnown || can("bill.draft")
  const mayCheckout = !roleKnown || can("bill.checkout")
  const mayExport = !roleKnown || can("report.view")
  const mayCancel = !roleKnown || can("bill.cancel")

  // Back/forward can swap the filters without going through the handlers; return to page 1 so we never land on an empty page.
  const filterKey = JSON.stringify([filters, status])
  const [seenFilterKey, setSeenFilterKey] = useState(filterKey)
  if (seenFilterKey !== filterKey) {
    setSeenFilterKey(filterKey)
    setPage(1)
  }

  // Resolved filter params; keyed on the primitive values so a re-render within
  // the same day does not refetch.
  const { date_from, date_to, partner_id, payment_status, invoice_status, q, mine } = toBillFilterParams(filters)
  const params = useMemo(
    () => ({ date_from, date_to, partner_id, payment_status, invoice_status, q, mine }),
    [date_from, date_to, partner_id, payment_status, invoice_status, q, mine],
  )

  useAbortableEffect(
    (signal, isCancelled) => {
      const rangeError = billRangeError(filters)
      if (rangeError) {
        setBills([])
        setTotal(0)
        setSummary(undefined)
        setError(rangeError)
        setLoading(false)
        return
      }
      setLoading(true)
      setError(null)
      listSaleBills({ page, size: PAGE_SIZE, status, ...params, tenantId, signal, retry: 2 })
        .then((res) => {
          if (isCancelled()) return
          setBills(res.items ?? [])
          setTotal(res.total)
          setSummary(res.summary)
        })
        .catch((e) => {
          if (isCancelled() || signal.aborted) return
          setError(extractApiError(e).message)
        })
        .finally(() => {
          if (isCancelled()) return
          setLoading(false)
        })
    },
    [tenantId, page, status, params, reloadTick, filters.range, filters.from, filters.to],
  )

  const reload = useCallback(() => setReloadTick((n) => n + 1), [])

  function handleFilterChange(patch: Partial<BillFilterState>) {
    setPage(1)
    // Settlement filters only apply to approved bills; drop a draft/cancelled tab the backend would reject.
    if ((patch.payment || patch.invoice) && (status === 0 || status === 9)) patch = { ...patch, status: undefined }
    update(patch)
  }

  function handleClearFilters() {
    setPage(1)
    reset()
  }

  function handleTabChange(s: BillStatus | undefined) {
    setPage(1)
    update(s === 0 || s === 9 ? { status: s, payment: "", invoice: "" } : { status: s })
  }

  async function handleCancel(bill: SaleBillHead) {
    try {
      await cancelSaleBill(bill.id, tenantId)
      reload()

      toast(`已取消销售单 ${bill.bill_no}`, {
        duration: 30_000,
        action: {
          label: "撤销",
          onClick: () => handleRestore(bill),
        },
      })
    } catch (e) {
      toast.error("取消失败：" + extractApiError(e).message)
    }
  }

  async function handleRestore(bill: SaleBillHead) {
    try {
      await restoreSaleBill(bill.id, tenantId)
      reload()
      router.refresh()
    } catch (e) {
      toast.error("撤销失败：" + extractApiError(e).message)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const exportDisabled = bills.length === 0 && !loading
  const exportHref = billsExportUrl("sale", { ...params, status })
  const filtered = hasActiveBillFilters(filters) || status !== undefined

  const columns: ColumnDef<SaleBillHead>[] = [
    {
      id: "bill_no",
      header: "单据号",
      cell: ({ row }) => (
        <>
          <span className="font-mono text-xs">{row.original.bill_no}</span>
          <span className="block text-xs text-muted-foreground md:hidden">
            {formatDate(row.original.bill_date)}
          </span>
        </>
      ),
    },
    {
      id: "bill_date",
      header: "日期",
      meta: { headerClassName: "hidden md:table-cell", cellClassName: "hidden md:table-cell" },
      cell: ({ row }) => (
        <span className="text-muted-foreground">{formatDate(row.original.bill_date)}</span>
      ),
    },
    {
      id: "partner",
      header: "客户",
      cell: ({ row }) => <span>{row.original.partner_name || (row.original.partner_id ? "—" : "散客")}</span>,
    },
    {
      id: "status",
      header: "状态",
      meta: { headerClassName: "hidden md:table-cell", cellClassName: "hidden md:table-cell" },
      cell: ({ row }) =>
        statusCell(BILL_STATUS_LABEL[row.original.status], BILL_STATUS_TONE[row.original.status]),
    },
    {
      id: "total_amount",
      header: "合计",
      meta: { align: "right" },
      cell: ({ row }) => currencyCell(row.original.total_amount),
    },
    {
      id: "paid_amount",
      header: "已收",
      meta: { align: "right", headerClassName: "hidden md:table-cell", cellClassName: "hidden md:table-cell" },
      cell: ({ row }) => currencyCell(row.original.paid_amount),
    },
    {
      id: "unpaid_amount",
      header: "未收",
      meta: { align: "right" },
      cell: ({ row }) => {
        // Only an approved bill carries a real debt; drafts and cancelled bills show a dash.
        const owed = row.original.status === 2 ? (row.original.unpaid_amount ?? row.original.receivable_amount) : undefined
        if (owed === undefined) return <span className="block text-right text-muted-foreground">—</span>
        const outstanding = parseFloat(owed) > 0
        return (
          <span
            className={`block text-right font-mono tabular-nums ${
              outstanding ? "text-warning" : "text-success"
            }`}
          >
            {formatCNY(outstanding ? owed : 0)}
          </span>
        )
      },
    },
    {
      id: "payment_status",
      header: "收款",
      cell: ({ row }) => <PaymentTag kind="sale" status={row.original.payment_status} />,
    },
    {
      id: "invoice_status",
      header: "开票",
      cell: ({ row }) => <InvoiceTag status={row.original.invoice_status} billStatus={row.original.status} />,
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-3">
          {row.original.status === 0 && mayCancel && (
            <button
              type="button"
              onClick={() => handleCancel(row.original)}
              className="text-xs text-destructive hover:underline"
            >
              取消
            </button>
          )}
          <Link
            href={`/sales/${row.original.id}`}
            className="text-xs text-primary hover:underline"
          >
            查看
          </Link>
        </div>
      ),
    },
  ]

  return (
    <PageContainer width="wide">
      <PageHeader
        title="销售单管理"
        actions={
          <>
            {!mayExport ? null : exportDisabled ? (
              <span
                title="暂无可导出数据"
                aria-disabled="true"
                className={buttonVariants({ variant: "outline", className: "pointer-events-none opacity-40" })}
              >
                导出当前筛选
              </span>
            ) : (
              <a href={exportHref} download className={buttonVariants({ variant: "outline" })}>
                导出当前筛选
              </a>
            )}
            {mayCheckout && (
              <Link href="/sales/new?mode=quick" className={buttonVariants()}>
                + 快速收银
              </Link>
            )}
            {mayDraft && (
              <Link href="/sales/new" className={buttonVariants({ variant: "outline" })}>
                + 新建销售单
              </Link>
            )}
          </>
        }
      />

      <BillListFilters kind="sale" filters={filters} onChange={handleFilterChange} />

      <Tabs items={STATUS_TABS} value={status} onValueChange={handleTabChange} className="mb-4" />

      <BillSummaryBar kind="sale" summary={summary} total={total} />

      <DataTable
        columns={columns}
        data={bills}
        loading={loading}
        error={error}
        getRowId={(b) => b.id}
        mobileCard={(b) => (
          <DataCard
            title={<span className="font-mono text-xs">{b.bill_no}</span>}
            subtitle={b.partner_name || (b.partner_id ? "—" : "散客")}
            aside={statusCell(BILL_STATUS_LABEL[b.status], BILL_STATUS_TONE[b.status])}
            fields={[
              { label: "日期", value: formatDate(b.bill_date) },
              ...[
                { label: "合计", value: formatCNY(b.total_amount) },
                { label: "收款", value: <PaymentTag kind="sale" status={b.payment_status} /> },
              ],
            ]}
            actions={
              <>
                {b.status === 0 && mayCancel && (
                  <button
                    type="button"
                    onClick={() => handleCancel(b)}
                    className="text-xs text-destructive hover:underline"
                  >
                    取消
                  </button>
                )}
                <Link href={`/sales/${b.id}`} className="text-xs text-primary hover:underline">
                  查看
                </Link>
              </>
            }
          />
        )}
        animateRows
        skeletonRows={5}
        empty={
          <EmptyState
            title="这个时间段没有单据"
            description={filtered ? "换个时间或条件再看看" : "创建第一笔销售单以开始出库"}
            action={
              <div className="flex items-center gap-4">
                <button
                  type="button"
                  onClick={handleClearFilters}
                  className="text-sm text-primary hover:underline"
                >
                  清除筛选
                </button>
                {!filtered && mayDraft && (
                  <Link href="/sales/new" className="text-sm text-primary hover:underline">
                    立即新建
                  </Link>
                )}
              </div>
            }
          />
        }
      />

      <Pagination page={page} totalPages={totalPages} onPageChange={setPage} />
    </PageContainer>
  )
}

export default function SalesPage() {
  return (
    <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">加载中...</div>}>
      <SalesPageInner />
    </Suspense>
  )
}
