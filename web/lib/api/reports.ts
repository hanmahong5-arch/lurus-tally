/**
 * API clients for the four analytics report endpoints.
 *
 * Backend routes (prefix /api/v1, proxied through /api/proxy):
 *   GET /reports/gross-margin?days=30
 *   GET /reports/abc
 *   GET /reports/dead-stock?days=90
 *   GET /reports/sales-top?metric=revenue&days=7&limit=10
 */
import { apiFetch } from "./client"

// ── Gross Margin ─────────────────────────────────────────────────────────────

export interface MarginProduct {
  name: string
  avg_margin: string // e.g. "42.3%"
}

export interface GrossMarginResult {
  overall_margin: string // e.g. "38.5%"
  top10: MarginProduct[]
  bottom10: MarginProduct[]
  days: number
}

export async function fetchGrossMargin(days = 30, signal?: AbortSignal): Promise<GrossMarginResult> {
  return apiFetch<GrossMarginResult>(`/reports/gross-margin?days=${days}`, { signal })
}

// ── ABC Classification ───────────────────────────────────────────────────────

export interface ABCTier {
  sku_count: number
  revenue_share: string // e.g. "80.0%"
}

export interface ABCResult {
  a: ABCTier
  b: ABCTier
  c: ABCTier
  total_skus: number
  period: string // "365d"
}

export async function fetchABC(signal?: AbortSignal): Promise<ABCResult> {
  return apiFetch<ABCResult>("/reports/abc", { signal })
}

// ── Dead Stock ───────────────────────────────────────────────────────────────

export interface DeadStockItem {
  name: string
  code: string
  qty: string
  value_cny: string
  days_since_last_movement: number
}

export interface DeadStockResult {
  items: DeadStockItem[]
  count: number
  threshold_days: number
}

export async function fetchDeadStock(days = 90, signal?: AbortSignal): Promise<DeadStockResult> {
  return apiFetch<DeadStockResult>(`/reports/dead-stock?days=${days}`, { signal })
}

// ── Sales Top-N ──────────────────────────────────────────────────────────────

export type SalesMetric = "revenue" | "margin" | "qty"

export interface SalesTopItem {
  rank: number
  name: string
  score: string
}

export interface SalesTopResult {
  top_products: SalesTopItem[]
  metric: SalesMetric
  days: number
}

export async function fetchSalesTop(
  metric: SalesMetric = "revenue",
  days = 7,
  limit = 10,
  signal?: AbortSignal,
): Promise<SalesTopResult> {
  return apiFetch<SalesTopResult>(
    `/reports/sales-top?metric=${metric}&days=${days}&limit=${limit}`,
    { signal },
  )
}

// ── Client-side CSV export helper ────────────────────────────────────────────

/**
 * Converts an array of objects to a CSV string and triggers a browser download.
 * All values are stringified; keys become the header row.
 */
export function downloadCSV(rows: Record<string, unknown>[], filename: string): void {
  if (rows.length === 0) return
  const keys = Object.keys(rows[0])
  const header = keys.join(",")
  const body = rows
    .map((r) => keys.map((k) => JSON.stringify(r[k] ?? "")).join(","))
    .join("\n")
  const blob = new Blob([header + "\n" + body], { type: "text/csv;charset=utf-8;" })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
