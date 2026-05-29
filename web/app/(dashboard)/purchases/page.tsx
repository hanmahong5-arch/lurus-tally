"use client"

import { useCallback, useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  listPurchaseBills,
  cancelPurchaseBill,
  restorePurchaseBill,
  type BillHead,
  type BillStatus,
  BILL_STATUS_LABEL,
} from "@/lib/api/purchase"
import { BILL_STATUS_TONE } from "@/lib/status"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable, currencyCell, statusCell } from "@/components/ui/data-table"
import { Tabs, type TabItem } from "@/components/ui/tabs"
import { Pagination } from "@/components/ui/pagination"
import { buttonVariants } from "@/components/ui/button"
import { EmptyState } from "@/components/ui/empty-state"
import { formatDate } from "@/lib/format"

const PAGE_SIZE = 20

const STATUS_TABS: TabItem<BillStatus | undefined>[] = [
  { label: "全部", value: undefined },
  { label: "草稿", value: 0 },
  { label: "已审核", value: 2 },
  { label: "已取消", value: 9 },
]

export default function PurchasesPage() {
  const router = useRouter()
  const [bills, setBills] = useState<BillHead[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<BillStatus | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const tenantId = useTenantId()

  const load = useCallback(
    (
      p: number,
      s: BillStatus | undefined,
      signal?: AbortSignal,
      isCancelled?: () => boolean,
    ) => {
      setLoading(true)
      setError(null)
      listPurchaseBills({ page: p, size: PAGE_SIZE, status: s, tenantId, signal, retry: 2 })
        .then((res) => {
          if (isCancelled?.()) return
          setBills(res.items ?? [])
          setTotal(res.total)
        })
        .catch((e) => {
          if (isCancelled?.() || signal?.aborted) return
          setError(String(e))
        })
        .finally(() => {
          if (isCancelled?.()) return
          setLoading(false)
        })
    },
    [tenantId],
  )

  useAbortableEffect((signal, isCancelled) => {
    load(1, undefined, signal, isCancelled)
  }, [load])

  function handleTabChange(s: BillStatus | undefined) {
    setStatus(s)
    setPage(1)
    load(1, s)
  }

  function handlePageChange(p: number) {
    setPage(p)
    load(p, status)
  }

  async function handleCancel(bill: BillHead) {
    try {
      await cancelPurchaseBill(bill.id, tenantId)
      load(page, status)

      toast(`已取消采购单 ${bill.bill_no}`, {
        duration: 30_000,
        action: {
          label: "撤销",
          onClick: () => handleRestore(bill),
        },
      })
    } catch (e) {
      toast.error("取消失败：" + String(e))
    }
  }

  async function handleRestore(bill: BillHead) {
    try {
      await restorePurchaseBill(bill.id, tenantId)
      load(page, status)
      router.refresh()
    } catch (e) {
      toast.error("撤销失败：" + String(e))
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const exportDisabled = bills.length === 0 && !loading

  const columns: ColumnDef<BillHead>[] = [
    {
      id: "bill_no",
      header: "单据号",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.bill_no}</span>,
    },
    {
      id: "status",
      header: "状态",
      cell: ({ row }) =>
        statusCell(BILL_STATUS_LABEL[row.original.status], BILL_STATUS_TONE[row.original.status]),
    },
    {
      id: "total_amount",
      header: "合计金额",
      meta: { align: "right" },
      cell: ({ row }) => currencyCell(row.original.total_amount),
    },
    {
      id: "bill_date",
      header: "单据日期",
      cell: ({ row }) => (
        <span className="text-muted-foreground">{formatDate(row.original.bill_date)}</span>
      ),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-3">
          {row.original.status === 0 && (
            <button
              type="button"
              onClick={() => handleCancel(row.original)}
              className="text-xs text-destructive hover:underline"
            >
              取消
            </button>
          )}
          <Link
            href={`/purchases/${row.original.id}`}
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
        title="采购单管理"
        subtitle={`共 ${total} 条采购单`}
        actions={
          <>
            {exportDisabled ? (
              <span
                title="暂无可导出数据"
                aria-disabled="true"
                className={buttonVariants({ variant: "outline", className: "pointer-events-none opacity-40" })}
              >
                导出 CSV
              </span>
            ) : (
              <a
                href="/api/v1/exports/bills.csv"
                download
                className={buttonVariants({ variant: "outline" })}
              >
                导出 CSV
              </a>
            )}
            <Link href="/purchases/new" className={buttonVariants()}>
              + 新建采购单
            </Link>
          </>
        }
      />

      <Tabs
        items={STATUS_TABS}
        value={status}
        onValueChange={handleTabChange}
        className="mb-4"
      />

      <DataTable
        columns={columns}
        data={bills}
        loading={loading}
        error={error}
        getRowId={(b) => b.id}
        animateRows
        empty={
          <EmptyState
            title="暂无采购单"
            description="创建第一笔采购单以开始入库"
            action={
              <Link href="/purchases/new" className="text-sm text-primary hover:underline">
                立即新建
              </Link>
            }
          />
        }
      />

      <Pagination page={page} totalPages={totalPages} onPageChange={handlePageChange} />
    </PageContainer>
  )
}
