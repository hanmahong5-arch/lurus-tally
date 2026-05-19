/**
 * API wrapper for the supplier endpoints (W3.D1).
 * Follows the same fetch + X-Tenant-ID header pattern as projects.ts.
 */
import { apiFetch } from "./client"

export interface SupplierItem {
  id: string
  tenantId: string
  code: string
  name: string
  contact: string
  phone: string
  email: string
  address: string
  remark: string
  createdAt: string
  updatedAt: string
}

export interface SupplierListParams {
  q?: string
  limit?: number
  offset?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export interface SupplierListResult {
  items: SupplierItem[]
  total: number
}

export type SupplierCreateInput = {
  code?: string
  name: string
  contact?: string
  phone?: string
  email?: string
  address?: string
  remark?: string
}

export type SupplierUpdateInput = Partial<SupplierCreateInput>

export async function listSuppliers(
  params: SupplierListParams = {}
): Promise<SupplierListResult> {
  const { q, limit = 20, offset = 0, tenantId, signal, retry } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  return apiFetch<SupplierListResult>(`/suppliers?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getSupplier(id: string, tenantId?: string): Promise<SupplierItem> {
  return apiFetch<SupplierItem>(`/suppliers/${id}`, { tenantId })
}

export async function createSupplier(
  input: SupplierCreateInput,
  tenantId?: string
): Promise<SupplierItem> {
  return apiFetch<SupplierItem>("/suppliers", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function updateSupplier(
  id: string,
  input: SupplierUpdateInput,
  tenantId?: string
): Promise<SupplierItem> {
  return apiFetch<SupplierItem>(`/suppliers/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteSupplier(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/suppliers/${id}`, { method: "DELETE", tenantId })
}

export async function restoreSupplier(id: string, tenantId?: string): Promise<SupplierItem> {
  return apiFetch<SupplierItem>(`/suppliers/${id}/restore`, { method: "POST", tenantId })
}
