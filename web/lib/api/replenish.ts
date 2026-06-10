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
  // Forecast-driven fields (v2 formula).
  lead_time_days: number
  in_transit: string
  rop: string
  safety_stock: string
  reason: string
  // Learning-driven fields (F1/F2) — optional: older backends omit them.
  last_purchase_price?: string
  lead_time_source?: "learned" | "configured" | "default"
  lead_time_samples?: number
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

// ----- Scorecard -----

export interface ReplenishScorecard {
  window_days: number
  suggestions_count: number
  adopted_count: number
  adoption_rate: number
  stockout_misses: number
}

/** GET /api/v1/replenish/scorecard — 28-day suggestion track record. */
export async function fetchScorecard(
  tenantId?: string,
  signal?: AbortSignal
): Promise<ReplenishScorecard> {
  return apiFetch("/replenish/scorecard", { tenantId, signal })
}

// ----- Draft-batch -----

export interface DraftBatchLine {
  product_id: string
  supplier_id?: string
  qty: string
}

export interface DraftBatchRequest {
  lines: DraftBatchLine[]
}

export interface DraftBatchResult {
  bill_id: string
  bill_no: string
  supplier_id?: string
  supplier_name?: string
  line_count: number
}

export async function draftBatch(
  body: DraftBatchRequest,
  tenantId?: string
): Promise<{ drafts: DraftBatchResult[]; count: number }> {
  return apiFetch("/replenish/draft-batch", {
    method: "POST",
    body: JSON.stringify(body),
    tenantId,
  })
}
