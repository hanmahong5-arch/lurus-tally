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
  const blob = new Blob(["\uFEFF" + header + "\n" + body], { type: "text/csv;charset=utf-8;" })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

// ── Month-end reconciliation ─────────────────────────────────────────────────

/** GET /reports/monthly?month=YYYY-MM — amounts are two-decimal strings. */
export interface MonthlyReport {
  month: string
  date_from: string
  date_to: string
  net_sales: string
  cost_of_goods: string
  gross_profit: string
  net_purchases: string
  receipts: string
  payments: string
  receivable: string
  payable: string
  inventory_value: string
}

export async function fetchMonthly(month: string, signal?: AbortSignal): Promise<MonthlyReport> {
  return apiFetch<MonthlyReport>(`/reports/monthly?month=${encodeURIComponent(month)}`, { signal })
}

const MONTH_RE = /^\d{4}-(0[1-9]|1[0-2])$/

/** The requested month when it is a valid YYYY-MM no later than `current`, else `current`. */
export function resolveMonth(raw: string | undefined, current: string): string {
  return raw && MONTH_RE.test(raw) && raw <= current ? raw : current
}

/** The month before YYYY-MM (December rolls back a year). */
export function previousMonth(ym: string): string {
  const [y, m] = ym.split("-").map(Number)
  return m === 1 ? `${y - 1}-12` : `${y}-${String(m - 1).padStart(2, "0")}`
}

/** CSV downloads for one month; the dates are inclusive days on the shop's calendar. */
export function monthlyCsvLinks(r: Pick<MonthlyReport, "month" | "date_from" | "date_to">) {
  // status=2 keeps drafts and cancelled bills out, so the file adds up to the net sales figure
  const range = `date_from=${r.date_from}&date_to=${r.date_to}`
  return {
    bills: `/api/proxy/exports/bills.csv?${range}&status=2`,
    payments: `/api/proxy/exports/payments.csv?${range}`,
    // only the rows the monthly report counts, so each file adds up to its card
    receipts: `/api/proxy/exports/payments.csv?${range}&direction=in&counted_only=1`,
    paymentsOut: `/api/proxy/exports/payments.csv?${range}&direction=out&counted_only=1`,
    // 月底应收 / 应付 are the totals of the ageing page, not a download
    receivable: "/payments?tab=sales",
    payable: "/payments?tab=purchases",
    stock: "/api/proxy/exports/stock.csv",
    summary: `/api/proxy/reports/monthly.csv?month=${r.month}`,
  }
}
