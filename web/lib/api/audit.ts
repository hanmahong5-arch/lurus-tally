/**
 * API wrapper for the shop operation log (GET /audit): who approved, voided or
 * changed which document. The server sends Chinese labels; `action` and
 * `resource` are machine values and must never be rendered.
 */
import { apiFetch } from "./client"

export interface AuditItem {
  id: string
  at: string
  actor_name: string
  action: string
  action_label: string
  resource: string
  resource_label: string
  resource_id: string
  resource_no: string
  summary: string
}

export interface AuditList {
  items: AuditItem[]
  total: number
}

/** Object types the log can be filtered by, with the labels the picker shows. */
export const AUDIT_RESOURCE_OPTIONS: { value: string; label: string }[] = [
  { value: "purchase_bill", label: "采购单" },
  { value: "sale_bill", label: "销售单" },
]

export interface ListAuditParams {
  limit?: number
  offset?: number
  resource?: string
  resourceId?: string
  from?: string
  to?: string
  signal?: AbortSignal
}

export async function listAudit(p: ListAuditParams = {}): Promise<AuditList> {
  const q = new URLSearchParams()
  if (p.limit) q.set("limit", String(p.limit))
  if (p.offset) q.set("offset", String(p.offset))
  if (p.resource) q.set("resource", p.resource)
  if (p.resourceId) q.set("resource_id", p.resourceId)
  if (p.from) q.set("from", p.from)
  if (p.to) q.set("to", p.to)
  const qs = q.toString()
  const r = await apiFetch<AuditList>(`/audit${qs ? `?${qs}` : ""}`, { signal: p.signal, silent: true })
  return { items: r.items ?? [], total: r.total ?? 0 }
}
