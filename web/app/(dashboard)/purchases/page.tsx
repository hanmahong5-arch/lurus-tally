"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import {
  listPurchaseBills,
  type BillHead,
  type BillStatus,
  BILL_STATUS_LABEL,
} from "@/lib/api/purchase"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const STATUS_TABS: { label: string; value: BillStatus | undefined }[] = [
  { label: "全部", value: undefined },
  { label: "草稿", value: 0 },
  { label: "已审核", value: 2 },
  { label: "已取消", value: 9 },
]

const STATUS_BADGE: Record<BillStatus, string> = {
  0: "bg-muted text-muted-foreground",
  2: "bg-green-500/10 text-green-600",
  9: "bg-red-500/10 text-red-500",
}

export default function PurchasesPage() {
  const [bills, setBills] = useState<BillHead[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<BillStatus | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  function load(p: number = page, s: BillStatus | undefined = status) {
    setLoading(true)
    setError(null)
    listPurchaseBills({ page: p, size: 20, status: s, tenantId: devTenantId })
      .then((res) => {
        setBills(res.items ?? [])
        setTotal(res.total)
      })
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load(1, undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function handleTabChange(s: BillStatus | undefined) {
    setStatus(s)
    setPage(1)
    load(1, s)
  }

  const totalPages = Math.max(1, Math.ceil(total / 20))

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold">采购单管理</h1>
          <p className="text-sm text-muted-foreground mt-0.5">共 {total} 条采购单</p>
        </div>
        <Link
          href="/purchases/new"
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          + 新建采购单
        </Link>
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
      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}
      {!loading && !error && bills.length === 0 && (
        <div className="py-12 text-center text-muted-foreground">
          暂无采购单，
          <Link href="/purchases/new" className="text-primary underline">
            立即新建
          </Link>
        </div>
      )}

      {!loading && bills.length > 0 && (
        <>
          <div className="overflow-hidden rounded-xl border border-border">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">单据号</th>
                  <th className="px-4 py-2.5 text-left font-medium">状态</th>
                  <th className="px-4 py-2.5 text-right font-medium">合计金额</th>
                  <th className="px-4 py-2.5 text-left font-medium">单据日期</th>
                  <th className="px-4 py-2.5 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {bills.map((b) => (
                  <tr key={b.id} className="hover:bg-muted/30 transition-colors">
                    <td className="px-4 py-2.5 font-mono text-xs">{b.bill_no}</td>
                    <td className="px-4 py-2.5">
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs ${STATUS_BADGE[b.status]}`}
                      >
                        {BILL_STATUS_LABEL[b.status]}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">{b.total_amount}</td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {new Date(b.bill_date).toLocaleDateString("zh-CN")}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      <Link
                        href={`/purchases/${b.id}`}
                        className="text-xs text-primary hover:underline"
                      >
                        查看
                      </Link>
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
                className="text-sm px-3 py-1 rounded border border-border hover:bg-muted disabled:opacity-40"
              >
                上一页
              </button>
              <span className="text-sm py-1 text-muted-foreground">
                {page} / {totalPages}
              </span>
              <button
                disabled={page >= totalPages}
                onClick={() => { setPage(page + 1); load(page + 1, status) }}
                className="text-sm px-3 py-1 rounded border border-border hover:bg-muted disabled:opacity-40"
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
