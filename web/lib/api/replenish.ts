/**
 * API wrapper for replenishment suggestion endpoints.
 * Endpoint: GET /api/v1/replenish/suggestions?weeks=<n>
 */
import { apiFetch } from "./client"

export interface ReplenishSuggestion {
  product_id: string
  product_name: string
  product_code: string
  available_qty: string
  safety_qty: string
  avg_daily_sales: string
  suggested_qty: string
  est_amount_cny: string
  supplier_id?: string
  supplier_name?: string
  urgency_score: string
}

export interface ListSuggestionsParams {
  weeks?: number
  tenantId?: string
  signal?: AbortSignal
}

export async function listReplenishSuggestions(
  params: ListSuggestionsParams = {}
): Promise<{ items: ReplenishSuggestion[]; count: number; weeks: number }> {
  const { weeks = 2, tenantId, signal } = params
  const usp = new URLSearchParams({ weeks: String(weeks) })
  return apiFetch(`/replenish/suggestions?${usp.toString()}`, { tenantId, signal })
}
