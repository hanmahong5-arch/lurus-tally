/**
 * API wrapper for the warehouse endpoints (W3.D1).
 * Follows the same fetch + X-Tenant-ID header pattern as projects.ts.
 */
import { apiFetch } from "./client"

export interface WarehouseItem {
  id: string
  tenantId: string
  code: string
  name: string
  address: string
  manager: string
  isDefault: boolean
  remark: string
  createdAt: string
  updatedAt: string
}

export interface WarehouseListParams {
  q?: string
  limit?: number
  offset?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export interface WarehouseListResult {
  items: WarehouseItem[]
  total: number
}

export type WarehouseCreateInput = {
  code?: string
  name: string
  address?: string
  manager?: string
  isDefault?: boolean
  remark?: string
}

export type WarehouseUpdateInput = Partial<WarehouseCreateInput>

export async function listWarehouses(
  params: WarehouseListParams = {}
): Promise<WarehouseListResult> {
  const { q, limit = 20, offset = 0, tenantId, signal, retry } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  return apiFetch<WarehouseListResult>(`/warehouses?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getWarehouse(id: string, tenantId?: string): Promise<WarehouseItem> {
  return apiFetch<WarehouseItem>(`/warehouses/${id}`, { tenantId })
}

export async function createWarehouse(
  input: WarehouseCreateInput,
  tenantId?: string
): Promise<WarehouseItem> {
  return apiFetch<WarehouseItem>("/warehouses", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function updateWarehouse(
  id: string,
  input: WarehouseUpdateInput,
  tenantId?: string
): Promise<WarehouseItem> {
  return apiFetch<WarehouseItem>(`/warehouses/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteWarehouse(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/warehouses/${id}`, { method: "DELETE", tenantId })
}

export async function restoreWarehouse(id: string, tenantId?: string): Promise<WarehouseItem> {
  return apiFetch<WarehouseItem>(`/warehouses/${id}/restore`, { method: "POST", tenantId })
}
