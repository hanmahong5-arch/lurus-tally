/**
 * API wrapper for the unit_def catalogue endpoints.
 */

export type UnitType =
  | "count"
  | "weight"
  | "length"
  | "volume"
  | "area"
  | "time"

export interface UnitDef {
  id: string
  tenant_id: string
  code: string
  name: string
  unit_type: UnitType
  is_system: boolean
  created_at: string
  updated_at: string
}

export interface CreateUnitInput {
  code: string
  name: string
  unit_type: UnitType
}

export interface ListUnitsResponse {
  items: UnitDef[]
}

const BASE = "/api/v1"

function headers(tenantId?: string): HeadersInit {
  const h: HeadersInit = { "Content-Type": "application/json" }
  if (tenantId) {
    ;(h as Record<string, string>)["X-Tenant-ID"] = tenantId
  }
  return h
}

export async function listUnits(
  unitType?: UnitType,
  tenantId?: string
): Promise<ListUnitsResponse> {
  const url = new URL(BASE + "/units", window.location.origin)
  if (unitType) url.searchParams.set("unit_type", unitType)

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `listUnits: HTTP ${res.status}`)
  }
  return res.json()
}

export async function createUnit(
  input: CreateUnitInput,
  tenantId?: string
): Promise<UnitDef> {
  const res = await fetch(`${BASE}/units`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `createUnit: HTTP ${res.status}`)
  }
  return res.json()
}

export async function deleteUnit(
  id: string,
  tenantId?: string
): Promise<void> {
  const res = await fetch(`${BASE}/units/${id}`, {
    method: "DELETE",
    headers: headers(tenantId),
  })
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `deleteUnit: HTTP ${res.status}`)
  }
}
