/**
 * API wrapper for currency and exchange rate endpoints.
 * Story 9.1: multi-currency support for cross_border profile.
 */

export interface Currency {
  code: string
  name: string
  symbol: string
  enabled: boolean
}

export interface ExchangeRate {
  id: string
  tenant_id: string
  from_currency: string
  to_currency: string
  rate: string
  source: string
  effective_at: string
  created_at: string
}

export interface RateResult {
  rate: string
  source: string
  warning?: string
}

export interface CreateRateRequest {
  from_currency: string
  to_currency: string
  rate: string
  effective_at?: string // RFC3339 or YYYY-MM-DD; defaults to today on server
}

const BASE = "/api/proxy"

async function handleResponse<T>(res: Response, operation: string): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.message ?? body.error ?? `${operation}: HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

/**
 * Returns all enabled currencies (CNY/USD/EUR/GBP/JPY/HKD).
 */
export async function getCurrencies(): Promise<Currency[]> {
  const res = await fetch(`${BASE}/currencies`)
  const data = await handleResponse<{ currencies: Currency[] }>(res, "getCurrencies")
  return data.currencies
}

/**
 * Returns the most recent exchange rate for the given pair on or before `date`.
 * Falls back to { rate: "1", source: "default", warning: "no_rate_found" } when no data exists.
 * @param from  - Source currency code (e.g. "USD")
 * @param to    - Target currency code (e.g. "CNY")
 * @param date  - Date string in YYYY-MM-DD format; defaults to today
 */
export async function getRateOn(from: string, to: string, date?: string): Promise<RateResult> {
  const url = new URL(BASE + "/exchange-rates", window.location.origin)
  url.searchParams.set("from", from)
  url.searchParams.set("to", to)
  if (date) {
    url.searchParams.set("date", date)
  }
  const res = await fetch(url.toString())
  return handleResponse<RateResult>(res, "getRateOn")
}

/**
 * Creates a manual exchange rate record.
 * Returns the created ExchangeRate (source is always "manual").
 */
export async function createRate(
  body: CreateRateRequest,
  tenantId?: string
): Promise<ExchangeRate> {
  const headers: Record<string, string> = { "Content-Type": "application/json" }
  if (tenantId) headers["X-Tenant-ID"] = tenantId

  const res = await fetch(`${BASE}/exchange-rates`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  })
  return handleResponse<ExchangeRate>(res, "createRate")
}

/**
 * Returns exchange rate history for a currency pair, ordered by effective_at ASC.
 * @param from - Source currency code
 * @param to   - Target currency code
 * @param days - Number of days of history (default 30, max 365)
 */
export async function getRateHistory(
  from: string,
  to: string,
  days = 30
): Promise<ExchangeRate[]> {
  const url = new URL(BASE + "/exchange-rates/history", window.location.origin)
  url.searchParams.set("from", from)
  url.searchParams.set("to", to)
  url.searchParams.set("days", String(days))
  const res = await fetch(url.toString())
  const data = await handleResponse<{ rates: ExchangeRate[] | null }>(res, "getRateHistory")
  return data.rates ?? []
}
