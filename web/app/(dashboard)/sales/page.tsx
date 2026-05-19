"use client"

import { useCallback, useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import {
  listSaleBills,
  cancelSaleBill,
  restoreSaleBill,
  type SaleBillHead,
  type BillStatus,
} from "@/lib/api/sale"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"
import { formatCNY } from "@/lib/format"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const BILL_STATUS_LABEL: Record<BillStatus, string> = {
  0: "草稿",
  2: "已审核",
  9: "已取消",
}

const STATUS_TABS: { label: string; value: BillStatus | undefined }[] = [
  { label: "全部", value: undefined },
  { label: "草稿", value: 0 },
  { label: "已审核", value: 2 },
  { label: "已取消", value: 9 },
]

const STATUS_BADGE: Record<BillStatus, string> = {
  0: "bg-muted text-muted-foreground",
  2: "bg-blue-500/10 text-blue-600",
  9: "bg-red-500/10 text-red-500",
}

export default function SalesPage() {
  const router = useRouter()
  const [bills, setBills] = useState<SaleBillHead[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<BillStatus | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback((
    p: number,
    s: BillStatus | undefined,
    signal?: AbortSignal,
    isCancelled?: () => boolean,
  ) => {
    setLoading(true)
    setError(null)
    listSaleBills({ page: p, size: 20, status: s, tenantId: devTenantId, signal, retry: 2 })
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
  }, [])

  useAbortableEffect((signal, isCancelled) => {
    load(1, undefined, signal, isCancelled)
  }, [load])

  function handleTabChange(s: BillStatus | undefined) {
    setStatus(s)
    setPage(1)
    load(1, s)
  }

  async function handleCancel(bill: SaleBillHead) {
    try {
      await cancelSaleBill(bill.id, devTenantId)
      load(page, status)

      toast(`已取消销售单 ${bill.bill_no}`, {
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

  async function handleRestore(bill: SaleBillHead) {
    try {
      await restoreSaleBill(bill.id, devTenantId)
      load(page, status)
      router.refresh()
    } catch (e) {
      toast.error("撤销失败：" + String(e))
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / 20))

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold">销售单管理</h1>
          <p className="text-sm text-muted-foreground mt-0.5">共 {total} 条销售单</p>
        </div>
        <div className="flex gap-2">
          <Link
            href="/sales/new?mode=quick"
            className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
          >
            + 快速收银
          </Link>
          <Link
            href="/sales/new"
            className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted transition-colors"
          >
            + 新建销售单
          </Link>
        </div>
      </div>

      {/* Status tabs */}
      <div className="flex gap-1 mb-4 border-b border-border">
        {STATUS_TABS.map((tab) => (
          <button
            key={tab.label}
            onClick={() => handleTabChange(tab.value)}
            className={`px-4 py-2 text-sm transition-colors border-b-2 -mb-px ${
              status === tab.value
                ? "border-primary text-primary font-medium"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {loading && (
        <div className="py-12 text-center text-muted-foreground">加载中...</div>
      )}
      {error && <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>}
      {!loading && !error && bills.length === 0 && (
        <EmptyState
          title="暂无销售单"
          description="创建第一笔销售单以开始出库"
          action={
            <Link href="/sales/new" className="text-sm text-primary hover:underline">
              立即新建
            </Link>
          }
        />
      )}

      {!loading && bills.length > 0 && (
        <>
          <div className="overflow-x-auto rounded-xl border border-border">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">单据号</th>
                  <th className="px-4 py-2.5 text-left font-medium">客户</th>
                  <th className="px-4 py-2.5 text-left font-medium">单据日期</th>
                  <th className="px-4 py-2.5 text-right font-medium">金额</th>
                  <th className="px-4 py-2.5 text-right font-medium">应收</th>
                  <th className="px-4 py-2.5 text-left font-medium">状态</th>
                  <th className="px-4 py-2.5 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {bills.map((b) => (
                  <tr key={b.id} className="hover:bg-muted/30 transition-colors">
                    <td className="px-4 py-2.5 font-mono text-xs">{b.bill_no}</td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {b.partner_id ? b.partner_id : "—"}
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {new Date(b.bill_date).toLocaleDateString("zh-CN")}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">
                      {formatCNY(b.total_amount)}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">
                      {parseFloat(b.receivable_amount) > 0 ? (
                        <span className="text-amber-600">{formatCNY(b.receivable_amount)}</span>
                      ) : (
                        <span className="text-green-600">{formatCNY(0)}</span>
                      )}
                    </td>
                    <td className="px-4 py-2.5">
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs ${STATUS_BADGE[b.status]}`}
                      >
                        {BILL_STATUS_LABEL[b.status]}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      <div className="flex items-center justify-end gap-2">
                        {b.status === 0 && (
                          <button
                            onClick={() => handleCancel(b)}
                            className="text-xs text-red-500 hover:underline"
                          >
                            取消
                          </button>
                        )}
                        <Link
                          href={`/sales/${b.id}`}
                          className="text-xs text-primary hover:underline"
                        >
                          查看
                        </Link>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex justify-center gap-2 mt-4">
              <button
                disabled={page <= 1}
                onClick={() => { setPage(page - 1); load(page - 1, status) }}
                className="text-sm px-3 py-1 rounded border border-border hover:bg-muted disabled:opacity-50"
              >
                上一页
              </button>
              <span className="text-sm py-1 text-muted-foreground">
                {page} / {totalPages}
              </span>
              <button
                disabled={page >= totalPages}
                onClick={() => { setPage(page + 1); load(page + 1, status) }}
                className="text-sm px-3 py-1 rounded border border-border hover:bg-muted disabled:opacity-50"
              >
                下一页
              </button>
            </div>
          )}
        </>
      )}
    </div>
  )
}
