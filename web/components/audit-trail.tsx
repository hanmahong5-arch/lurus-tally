"use client"

import { useEffect, useState } from "react"
import { listAudit, type AuditItem } from "@/lib/api/audit"
import { extractApiError } from "@/lib/api/errors"
import { formatDateTime } from "@/lib/format"

interface AuditTrailProps {
  /** The document whose history to show. */
  resourceId: string
}

/**
 * AuditTrail is the collapsed "操作记录" block at the bottom of a bill page. The
 * list is only fetched when the block is opened, so closed pages cost nothing.
 * Mount it only for roles holding audit.view.
 */
export function AuditTrail({ resourceId }: AuditTrailProps) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<AuditItem[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || !resourceId) return
    const controller = new AbortController()
    setError(null)
    listAudit({ resourceId, limit: 50, signal: controller.signal })
      .then((r) => setItems(r.items))
      .catch((e) => {
        if (controller.signal.aborted) return
        setError(extractApiError(e).message)
      })
    return () => controller.abort()
  }, [open, resourceId])

  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between text-sm font-medium text-muted-foreground"
      >
        <span>操作记录</span>
        <span aria-hidden="true">{open ? "收起" : "展开"}</span>
      </button>
      {open && (
        <div className="mt-3 text-sm">
          {error && <p role="alert" className="text-destructive">{error}</p>}
          {!error && items === null && <p className="text-muted-foreground">加载中…</p>}
          {!error && items !== null && items.length === 0 && (
            <p className="text-muted-foreground">这张单还没有操作记录。</p>
          )}
          {!error && items !== null && items.length > 0 && (
            <ul className="space-y-1.5">
              {items.map((it) => (
                <li key={it.id}>
                  <span className="tabular-nums text-muted-foreground">{formatDateTime(it.at)}</span>
                  {"　"}
                  {it.actor_name}
                  {"　"}
                  {it.action_label}
                  {it.summary ? `　${it.summary}` : ""}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
