"use client"

import { useEffect, useState } from "react"
import { listAudit, AUDIT_RESOURCE_OPTIONS, type AuditItem } from "@/lib/api/audit"
import { extractApiError } from "@/lib/api/errors"
import { useRole } from "@/hooks/use-role"
import { formatDateTime } from "@/lib/format"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"
import { Pagination } from "@/components/ui/pagination"
import { Skeleton } from "@/components/ui/skeleton"

const PAGE_SIZE = 20
const SELECT_CLASS =
  "h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

/** 操作记录: who approved, voided or changed which document (owner, manager, finance). */
export default function AuditPage() {
  const { known, can } = useRole()
  const allowed = !known || can("audit.view")
  const [resource, setResource] = useState("")
  const [page, setPage] = useState(1)
  const [items, setItems] = useState<AuditItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!allowed) return
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    listAudit({ resource, limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE, signal: controller.signal })
      .then((r) => {
        setItems(r.items)
        setTotal(r.total)
      })
      .catch((e) => {
        if (controller.signal.aborted) return
        setError(extractApiError(e).message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [resource, page, allowed])

  if (!allowed) {
    return (
      <PageContainer>
        <PageHeader title="操作记录" />
        <EmptyState title="你的角色没有查看操作记录的权限" description="请联系老板或店长" />
      </PageContainer>
    )
  }

  return (
    <PageContainer>
      <PageHeader title="操作记录" subtitle="谁审核、谁作废、谁改了什么" />
      <div className="mb-4 flex items-center gap-2 text-sm">
        <label htmlFor="audit-resource" className="text-muted-foreground">对象类型</label>
        <select
          id="audit-resource"
          className={SELECT_CLASS}
          value={resource}
          onChange={(e) => {
            setResource(e.target.value)
            setPage(1)
          }}
        >
          <option value="">全部</option>
          {AUDIT_RESOURCE_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>{o.label}</option>
          ))}
        </select>
      </div>

      {error && <ErrorBanner>{error}</ErrorBanner>}
      {loading && <Skeleton className="h-40 w-full" />}
      {!loading && !error && items.length === 0 && (
        <EmptyState title="还没有操作记录" description="审核、作废或修改单据后，这里会留下记录" />
      )}
      {!loading && items.length > 0 && (
        <div className="overflow-x-auto rounded-xl border border-border bg-card">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">时间</th>
                <th className="px-3 py-2 font-medium">操作人</th>
                <th className="px-3 py-2 font-medium">动作</th>
                <th className="px-3 py-2 font-medium">对象</th>
                <th className="px-3 py-2 font-medium">说明</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <tr key={it.id} className="border-b border-border last:border-0">
                  <td className="px-3 py-2 tabular-nums">{formatDateTime(it.at)}</td>
                  <td className="px-3 py-2">{it.actor_name}</td>
                  <td className="px-3 py-2">{it.action_label}</td>
                  <td className="px-3 py-2">
                    {it.resource_label}
                    {it.resource_no ? ` ${it.resource_no}` : ""}
                  </td>
                  <td className="px-3 py-2 text-muted-foreground">{it.summary || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Pagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} onPageChange={setPage} />
    </PageContainer>
  )
}
