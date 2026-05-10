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

export async function listProjects(
  params: ProjectListParams = {}
): Promise<ProjectListResult> {
  const { q, status, customerId, limit = 20, offset = 0, tenantId } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  if (status) usp.set("status", status)
  if (customerId) usp.set("customer_id", customerId)
  return apiFetch<ProjectListResult>(`/projects?${usp.toString()}`, { tenantId })
}

export async function getProject(id: string, tenantId?: string): Promise<ProjectItem> {
  return apiFetch<ProjectItem>(`/projects/${id}`, { tenantId })
}

export async function createProject(
  input: ProjectCreateInput,
  tenantId?: string
): Promise<ProjectItem> {
  return apiFetch<ProjectItem>("/projects", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function updateProject(
  id: string,
  input: ProjectUpdateInput,
  tenantId?: string
): Promise<ProjectItem> {
  return apiFetch<ProjectItem>(`/projects/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteProject(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/projects/${id}`, { method: "DELETE", tenantId })
}

export async function restoreProject(id: string, tenantId?: string): Promise<ProjectItem> {
  return apiFetch<ProjectItem>(`/projects/${id}/restore`, { method: "POST", tenantId })
}
