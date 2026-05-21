"use client"

import Link from "next/link"
import { useState } from "react"
import type { ColumnDef } from "@tanstack/react-table"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Tabs } from "@/components/ui/tabs"
import { DataTable } from "@/components/ui/data-table"
import { EmptyState } from "@/components/ui/empty-state"
import { listSaleBills, type SaleBillHead } from "@/lib/api/sale"
import { listPurchaseBills, type BillHead } from "@/lib/api/purchase"
import { formatCNY } from "@/lib/format"
import { cn } from "@/lib/utils"

/**
 * /payments — collections / payables 对账闭环.
 *
 * The existing GET /payments backend requires bill_id, so this page presents
 * the same data via the bill-level view: recent approved sales (应收) and
 * purchases (应付). Click into a bill to record a new payment or audit trail.
 */
export default function PaymentsPage() {
  const [tab, setTab] = useState<"sales" | "purchases">("sales")

  return (
    <PageContainer width="default">
      <PageHeader
        title="付款 / 对账"
        subtitle="已审核的销售单（应收）和采购单（应付）。点击单据进入详情录入收付款。"
      />

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

      {tab === "sales" ? <SalesView /> : <PurchasesView />}
    </PageContainer>
  )
}

function SalesView() {
  const [bills, setBills] = useState<SaleBillHead[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useAbortableEffect((signal, isCancelled) => {
    setLoading(true)
    setError(null)
    listSaleBills({ page: 1, size: 50, status: 2, signal })
      .then((r) => {
        if (isCancelled()) return
        setBills(r.items)
      })
      .catch((err: Error) => {
        if (isCancelled() || signal.aborted) return
        setError(err.message)
      })
      .finally(() => {
        if (isCancelled()) return
        setLoading(false)
      })
  }, [])

  const columns: ColumnDef<SaleBillHead>[] = [
    {
      id: "bill_no",
      header: "单号",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.bill_no}</span>,
    },
    {
      id: "date",
      header: "日期",
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">{row.original.bill_date?.slice(0, 10) ?? "—"}</span>
      ),
    },
    {
      id: "total",
      header: "金额",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="block text-right font-mono tabular-nums">
          {formatCNY(Number(row.original.total_amount) || 0)}
        </span>
      ),
    },
    {
      id: "paid",
      header: "已收",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="block text-right font-mono tabular-nums text-success">
          {formatCNY(Number(row.original.paid_amount) || 0)}
        </span>
      ),
    },
    {
      id: "outstanding",
      header: "未收",
      meta: { align: "right" },
      cell: ({ row }) => {
        const outstanding = (Number(row.original.total_amount) || 0) - (Number(row.original.paid_amount) || 0)
        return (
          <span
            className={cn(
              "block text-right font-mono tabular-nums",
              outstanding > 0 ? "text-warning" : "text-muted-foreground"
            )}
          >
            {formatCNY(outstanding)}
          </span>
        )
      },
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <Link href={`/sales/${row.original.id}`} className="text-xs text-primary hover:underline">
          详情
        </Link>
      ),
    },
  ]

  return (
    <DataTable
      columns={columns}
      data={bills}
      loading={loading}
      error={error}
      getRowId={(b) => b.id}
      skeletonRows={8}
      animateRows
      empty={<EmptyState title="暂无已审核销售单" description="销售单审核通过后会出现在这里。" />}
    />
  )
}

function PurchasesView() {
  const [bills, setBills] = useState<BillHead[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useAbortableEffect((signal, isCancelled) => {
    setLoading(true)
    setError(null)
    listPurchaseBills({ page: 1, size: 50, status: 2, signal })
      .then((r) => {
        if (isCancelled()) return
        setBills(r.items)
      })
      .catch((err: Error) => {
        if (isCancelled() || signal.aborted) return
        setError(err.message)
      })
      .finally(() => {
        if (isCancelled()) return
        setLoading(false)
      })
  }, [])

  const columns: ColumnDef<BillHead>[] = [
    {
      id: "bill_no",
      header: "单号",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.bill_no}</span>,
    },
    {
      id: "date",
      header: "日期",
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">{row.original.bill_date?.slice(0, 10) ?? "—"}</span>
      ),
    },
    {
      id: "total",
      header: "金额",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="block text-right font-mono tabular-nums">
          {formatCNY(Number(row.original.total_amount) || 0)}
        </span>
      ),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <Link href={`/purchases/${row.original.id}`} className="text-xs text-primary hover:underline">
          详情
        </Link>
      ),
    },
  ]

  return (
    <DataTable
      columns={columns}
      data={bills}
      loading={loading}
      error={error}
      getRowId={(b) => b.id}
      skeletonRows={8}
      animateRows
      empty={<EmptyState title="暂无已审核采购单" description="采购单审核通过后会出现在这里。" />}
    />
  )
}
