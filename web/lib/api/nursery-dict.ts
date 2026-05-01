/**
 * API wrapper for the nursery dictionary endpoints (Story 28.1).
 * Follows the same fetch + X-Tenant-ID header pattern as products.ts.
 */

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

const BASE = "/api/proxy"

function headers(tenantId?: string): HeadersInit {
  const h: HeadersInit = { "Content-Type": "application/json" }
  if (tenantId) {
    ;(h as Record<string, string>)["X-Tenant-ID"] = tenantId
  }
  return h
}

export async function listNurseryDict(
  params: NurseryDictListParams = {}
): Promise<NurseryDictListResult> {
  const { q, type, isEvergreen, limit = 20, offset = 0, tenantId } = params
  const url = new URL(BASE + "/nursery-dict", window.location.origin)
  if (q) url.searchParams.set("q", q)
  if (type) url.searchParams.set("type", type)
  if (isEvergreen !== undefined)
    url.searchParams.set("is_evergreen", String(isEvergreen))
  url.searchParams.set("limit", String(limit))
  url.searchParams.set("offset", String(offset))

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? `listNurseryDict: HTTP ${res.status}`)
  }
  return res.json() as Promise<NurseryDictListResult>
}

export async function getNurseryDict(
  id: string,
  tenantId?: string
): Promise<NurseryDictItem> {
  const res = await fetch(`${BASE}/nursery-dict/${id}`, {
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? `getNurseryDict: HTTP ${res.status}`)
  }
  return res.json() as Promise<NurseryDictItem>
}

export async function createNurseryDict(
  input: NurseryDictCreateInput,
  tenantId?: string
): Promise<NurseryDictItem> {
  const res = await fetch(`${BASE}/nursery-dict`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? `createNurseryDict: HTTP ${res.status}`)
  }
  return res.json() as Promise<NurseryDictItem>
}

export async function updateNurseryDict(
  id: string,
  input: Partial<NurseryDictCreateInput>,
  tenantId?: string
): Promise<NurseryDictItem> {
  const res = await fetch(`${BASE}/nursery-dict/${id}`, {
    method: "PUT",
    headers: headers(tenantId),
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? `updateNurseryDict: HTTP ${res.status}`)
  }
  return res.json() as Promise<NurseryDictItem>
}

export async function deleteNurseryDict(
  id: string,
  tenantId?: string
): Promise<void> {
  const res = await fetch(`${BASE}/nursery-dict/${id}`, {
    method: "DELETE",
    headers: headers(tenantId),
  })
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? `deleteNurseryDict: HTTP ${res.status}`)
  }
}

export async function restoreNurseryDict(
  id: string,
  tenantId?: string
): Promise<NurseryDictItem> {
  const res = await fetch(`${BASE}/nursery-dict/${id}/restore`, {
    method: "POST",
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? `restoreNurseryDict: HTTP ${res.status}`)
  }
  return res.json() as Promise<NurseryDictItem>
}
