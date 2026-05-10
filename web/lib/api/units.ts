/**
 * API wrapper for the unit_def catalogue endpoints.
 */
import { apiFetch } from "./client"

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

export async function listUnits(
  unitType?: UnitType,
  tenantId?: string
): Promise<ListUnitsResponse> {
  const qs = unitType ? `?unit_type=${encodeURIComponent(unitType)}` : ""
  return apiFetch<ListUnitsResponse>("/units" + qs, { tenantId })
}

export async function createUnit(
  input: CreateUnitInput,
  tenantId?: string
): Promise<UnitDef> {
  return apiFetch<UnitDef>("/units", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteUnit(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/units/${id}`, { method: "DELETE", tenantId })
}
