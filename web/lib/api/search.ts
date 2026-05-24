/**
 * Client for the entity search endpoint used by the ⌘K command palette.
 * GET /api/v1/search?q=&limit=
 */
import { apiFetch } from "./client"

export type EntityType = "product" | "supplier" | "customer" | "bill"

export interface EntityResult {
  type: EntityType
  id: string
  label: string
  sublabel: string
}

export interface EntityGroup {
  type: EntityType
  items: EntityResult[]
}

export interface SearchResponse {
  groups: EntityGroup[]
}

export async function searchEntities(
  q: string,
  opts: { limit?: number; tenantId?: string; signal?: AbortSignal } = {}
): Promise<SearchResponse> {
  const { limit = 5, tenantId, signal } = opts
  const usp = new URLSearchParams({ q, limit: String(limit) })
  return apiFetch<SearchResponse>(`/search?${usp.toString()}`, {
    tenantId,
    signal,
    silent: true, // palette errors are swallowed; no toast on transient failures
  })
}
