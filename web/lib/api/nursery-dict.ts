/**
 * API wrapper for the nursery dictionary endpoints (Story 28.1).
 * Follows the same fetch + X-Tenant-ID header pattern as products.ts.
 */
import { apiFetch } from "./client"

export type NurseryType =
  | "tree"
  | "shrub"
  | "herb"
  | "vine"
  | "bamboo"
  | "aquatic"
  | "bulb"
  | "fruit"

export interface NurseryDictItem {
  id: string
  tenant_id: string
  name: string
  latin_name: string
  family: string
  genus: string
  type: NurseryType
  is_evergreen: boolean
  climate_zones: string[]
  best_season: [number, number]
  spec_template: Record<string, unknown>
  default_unit_id?: string
  photo_url: string
  remark: string
  created_at: string
  updated_at: string
}

export interface NurseryDictListParams {
  q?: string
  type?: NurseryType
  isEvergreen?: boolean
  limit?: number
  offset?: number
  tenantId?: string
}

export interface NurseryDictListResult {
  items: NurseryDictItem[]
  total: number
}

export type NurseryDictCreateInput = Omit<
  NurseryDictItem,
  "id" | "tenant_id" | "created_at" | "updated_at"
>

export async function listNurseryDict(
  params: NurseryDictListParams = {}
): Promise<NurseryDictListResult> {
  const { q, type, isEvergreen, limit = 20, offset = 0, tenantId } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  if (type) usp.set("type", type)
  if (isEvergreen !== undefined) usp.set("is_evergreen", String(isEvergreen))
  return apiFetch<NurseryDictListResult>(`/nursery-dict?${usp.toString()}`, { tenantId })
}

export async function getNurseryDict(id: string, tenantId?: string): Promise<NurseryDictItem> {
  return apiFetch<NurseryDictItem>(`/nursery-dict/${id}`, { tenantId })
}

export async function createNurseryDict(
  input: NurseryDictCreateInput,
  tenantId?: string
): Promise<NurseryDictItem> {
  return apiFetch<NurseryDictItem>("/nursery-dict", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function updateNurseryDict(
  id: string,
  input: Partial<NurseryDictCreateInput>,
  tenantId?: string
): Promise<NurseryDictItem> {
  return apiFetch<NurseryDictItem>(`/nursery-dict/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteNurseryDict(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/nursery-dict/${id}`, { method: "DELETE", tenantId })
}

export async function restoreNurseryDict(
  id: string,
  tenantId?: string
): Promise<NurseryDictItem> {
  return apiFetch<NurseryDictItem>(`/nursery-dict/${id}/restore`, {
    method: "POST",
    tenantId,
  })
}
