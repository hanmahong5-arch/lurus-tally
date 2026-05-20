"use client"

import Link from "next/link"
import { useState } from "react"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorBanner } from "@/components/ui/error-banner"
import { TableSkeleton } from "@/components/ui/table-skeleton"
import { listSaleBills, type SaleBillHead } from "@/lib/api/sale"
import { listPurchaseBills, type BillHead } from "@/lib/api/purchase"
import { formatCNY } from "@/lib/format"

/**
 * /payments — collections / payables 对账闭环.
 *
 * The existing GET /payments backend requires bill_id, so this page presents
 * the same data via the bill-level view: recent approved sales (应收) and
 * purchases (应付), each with paid / unpaid columns. Click into a bill to
 * record a new payment or see the full audit trail.
 */
export default function PaymentsPage() {
  const [tab, setTab] = useState<"sales" | "purchases">("sales")

  return (
    <div className="mx-auto max-w-5xl px-6 py-6">
      <header className="mb-4">
        <h1 className="text-xl font-semibold">付款 / 对账</h1>
        <p className="mt-0.5 text-sm text-muted-foreground">
          已审核的销售单（应收）和采购单（应付）。点击单据进入详情录入收付款。
        </p>
      </header>

      <div className="mb-4 inline-flex rounded-lg border border-border p-0.5 text-sm">
        <TabButton active={tab === "sales"} onClick={() => setTab("sales")}>
          应收（销售）
        </TabButton>
        <TabButton active={tab === "purchases"} onClick={() => setTab("purchases")}>
          应付（采购）
        </TabButton>
      </div>

      {tab === "sales" ? <SalesView /> : <PurchasesView />}
    </div>
  )
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-md px-3 py-1.5 text-xs transition-colors ${
        active
          ? "bg-primary text-primary-foreground"
          : "text-muted-foreground hover:text-foreground"
      }`}
    >
      {children}
    </button>
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

  if (loading) return <TableSkeleton rows={8} />
  if (error) return <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>
  if (bills.length === 0) {
    return <EmptyState title="暂无已审核销售单" description="销售单审核通过后会出现在这里。" />
  }

  return (
    <div className="overflow-x-auto rounded-xl border border-border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50 text-muted-foreground">
          <tr>
            <th className="px-4 py-2.5 text-left font-medium">单号</th>
            <th className="px-4 py-2.5 text-left font-medium">日期</th>
            <th className="px-4 py-2.5 text-right font-medium">金额</th>
            <th className="px-4 py-2.5 text-right font-medium">已收</th>
            <th className="px-4 py-2.5 text-right font-medium">未收</th>
            <th className="px-4 py-2.5 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {bills.map((b) => {
            const paid = Number(b.paid_amount) || 0
            const total = Number(b.total_amount) || 0
            const outstanding = total - paid
            return (
              <tr key={b.id} className="hover:bg-muted/30">
                <td className="px-4 py-2.5 font-mono text-xs">{b.bill_no}</td>
                <td className="px-4 py-2.5 text-xs text-muted-foreground">
                  {b.bill_date?.slice(0, 10) ?? "—"}
                </td>
                <td className="px-4 py-2.5 text-right font-mono tabular-nums">
                  {formatCNY(total)}
                </td>
                <td className="px-4 py-2.5 text-right font-mono tabular-nums text-emerald-600">
                  {formatCNY(paid)}
                </td>
                <td
                  className={`px-4 py-2.5 text-right font-mono tabular-nums ${
                    outstanding > 0 ? "text-amber-600" : "text-muted-foreground"
                  }`}
                >
                  {formatCNY(outstanding)}
                </td>
                <td className="px-4 py-2.5 text-right">
                  <Link href={`/sales/${b.id}`} className="text-xs text-primary hover:underline">
                    详情
                  </Link>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
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

  if (loading) return <TableSkeleton rows={8} />
  if (error) return <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>
  if (bills.length === 0) {
    return <EmptyState title="暂无已审核采购单" description="采购单审核通过后会出现在这里。" />
  }

  return (
    <div className="overflow-x-auto rounded-xl border border-border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50 text-muted-foreground">
          <tr>
            <th className="px-4 py-2.5 text-left font-medium">单号</th>
            <th className="px-4 py-2.5 text-left font-medium">日期</th>
            <th className="px-4 py-2.5 text-right font-medium">金额</th>
            <th className="px-4 py-2.5 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {bills.map((b) => (
            <tr key={b.id} className="hover:bg-muted/30">
              <td className="px-4 py-2.5 font-mono text-xs">{b.bill_no}</td>
              <td className="px-4 py-2.5 text-xs text-muted-foreground">
                {b.bill_date?.slice(0, 10) ?? "—"}
              </td>
              <td className="px-4 py-2.5 text-right font-mono tabular-nums">
                {formatCNY(Number(b.total_amount) || 0)}
              </td>
              <td className="px-4 py-2.5 text-right">
                <Link
                  href={`/purchases/${b.id}`}
                  className="text-xs text-primary hover:underline"
                >
                  详情
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
