/**
 * API wrapper for the project endpoints (Story 28.2).
 * Follows the same fetch + X-Tenant-ID header pattern as nursery-dict.ts.
 */
import { apiFetch } from "./client"

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
  signal?: AbortSignal
  retry?: number
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

// 后端字段是下划线命名，页面用驼峰；进出都在这里转换，调用方不用关心。
interface ProjectWire {
  id: string
  tenant_id: string
  code: string
  name: string
  customer_id?: string
  contract_amount?: string
  start_date?: string
  end_date?: string
  status: ProjectStatus
  address: string
  manager: string
  remark: string
  created_at: string
  updated_at: string
}

function fromWire(w: ProjectWire): ProjectItem {
  return {
    id: w.id,
    tenantId: w.tenant_id,
    code: w.code,
    name: w.name,
    customerId: w.customer_id,
    contractAmount: w.contract_amount,
    startDate: w.start_date,
    endDate: w.end_date,
    status: w.status,
    address: w.address,
    manager: w.manager,
    remark: w.remark,
    createdAt: w.created_at,
    updatedAt: w.updated_at,
  }
}

function toWire(input: ProjectUpdateInput): Record<string, unknown> {
  const { customerId, contractAmount, startDate, endDate, ...rest } = input
  return {
    ...rest,
    customer_id: customerId,
    contract_amount: contractAmount,
    start_date: startDate,
    end_date: endDate,
  }
}

export async function listProjects(
  params: ProjectListParams = {}
): Promise<ProjectListResult> {
  const { q, status, customerId, limit = 20, offset = 0, tenantId, signal, retry } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  if (status) usp.set("status", status)
  if (customerId) usp.set("customer_id", customerId)
  const res = await apiFetch<{ items: ProjectWire[] | null; total: number }>(
    `/projects?${usp.toString()}`,
    { tenantId, signal, retry }
  )
  return { items: (res.items ?? []).map(fromWire), total: res.total }
}

export async function getProject(id: string, tenantId?: string): Promise<ProjectItem> {
  return fromWire(await apiFetch<ProjectWire>(`/projects/${id}`, { tenantId }))
}

export async function createProject(
  input: ProjectCreateInput,
  tenantId?: string
): Promise<ProjectItem> {
  return fromWire(
    await apiFetch<ProjectWire>("/projects", {
      method: "POST",
      body: JSON.stringify(toWire(input)),
      tenantId,
    })
  )
}

export async function updateProject(
  id: string,
  input: ProjectUpdateInput,
  tenantId?: string
): Promise<ProjectItem> {
  return fromWire(
    await apiFetch<ProjectWire>(`/projects/${id}`, {
      method: "PUT",
      body: JSON.stringify(toWire(input)),
      tenantId,
    })
  )
}

export async function deleteProject(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/projects/${id}`, { method: "DELETE", tenantId })
}

export async function restoreProject(id: string, tenantId?: string): Promise<ProjectItem> {
  return fromWire(await apiFetch<ProjectWire>(`/projects/${id}/restore`, { method: "POST", tenantId }))
}
