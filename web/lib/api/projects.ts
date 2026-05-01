/**
 * API wrapper for the project endpoints (Story 28.2).
 * Follows the same fetch + X-Tenant-ID header pattern as nursery-dict.ts.
 */

export type ProjectStatus = "active" | "paused" | "completed" | "cancelled"

export interface ProjectItem {
  id: string
  tenantId: string
  code: string
  name: string
  customerId?: string
  contractAmount?: string
  startDate?: string // "YYYY-MM-DD"
  endDate?: string
  status: ProjectStatus
  address: string
  manager: string
  remark: string
  createdAt: string
  updatedAt: string
}

export interface ProjectListParams {
  q?: string
  status?: ProjectStatus
  customerId?: string
  limit?: number
  offset?: number
  tenantId?: string
}

export interface ProjectListResult {
  items: ProjectItem[]
  total: number
}

export type ProjectCreateInput = Omit<
  ProjectItem,
  "id" | "tenantId" | "createdAt" | "updatedAt"
>
export type ProjectUpdateInput = Partial<ProjectCreateInput>

const BASE = "/api/proxy"

function headers(tenantId?: string): HeadersInit {
  const h: HeadersInit = { "Content-Type": "application/json" }
  if (tenantId) {
    ;(h as Record<string, string>)["X-Tenant-ID"] = tenantId
  }
  return h
}

export async function listProjects(
  params: ProjectListParams = {}
): Promise<ProjectListResult> {
  const { q, status, customerId, limit = 20, offset = 0, tenantId } = params
  const url = new URL(BASE + "/projects", window.location.origin)
  if (q) url.searchParams.set("q", q)
  if (status) url.searchParams.set("status", status)
  if (customerId) url.searchParams.set("customer_id", customerId)
  url.searchParams.set("limit", String(limit))
  url.searchParams.set("offset", String(offset))

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(
      (body as { error?: string }).error ?? `listProjects: HTTP ${res.status}`
    )
  }
  return res.json() as Promise<ProjectListResult>
}

export async function getProject(
  id: string,
  tenantId?: string
): Promise<ProjectItem> {
  const res = await fetch(`${BASE}/projects/${id}`, {
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(
      (body as { error?: string }).error ?? `getProject: HTTP ${res.status}`
    )
  }
  return res.json() as Promise<ProjectItem>
}

export async function createProject(
  input: ProjectCreateInput,
  tenantId?: string
): Promise<ProjectItem> {
  const res = await fetch(`${BASE}/projects`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(
      (body as { error?: string }).error ?? `createProject: HTTP ${res.status}`
    )
  }
  return res.json() as Promise<ProjectItem>
}

export async function updateProject(
  id: string,
  input: ProjectUpdateInput,
  tenantId?: string
): Promise<ProjectItem> {
  const res = await fetch(`${BASE}/projects/${id}`, {
    method: "PUT",
    headers: headers(tenantId),
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(
      (body as { error?: string }).error ?? `updateProject: HTTP ${res.status}`
    )
  }
  return res.json() as Promise<ProjectItem>
}

export async function deleteProject(
  id: string,
  tenantId?: string
): Promise<void> {
  const res = await fetch(`${BASE}/projects/${id}`, {
    method: "DELETE",
    headers: headers(tenantId),
  })
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({}))
    throw new Error(
      (body as { error?: string }).error ?? `deleteProject: HTTP ${res.status}`
    )
  }
}

export async function restoreProject(
  id: string,
  tenantId?: string
): Promise<ProjectItem> {
  const res = await fetch(`${BASE}/projects/${id}/restore`, {
    method: "POST",
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(
      (body as { error?: string }).error ?? `restoreProject: HTTP ${res.status}`
    )
  }
  return res.json() as Promise<ProjectItem>
}
