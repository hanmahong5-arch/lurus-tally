/**
 * API wrapper for POS-related endpoints.
 * Amounts are represented as strings to avoid JSON floating-point loss.
 * Story 2.1 TODO: replace tenantId param with session-cookie auth.
 */

const BASE = "/api/proxy"

function headers(tenantId?: string): Record<string, string> {
  const h: Record<string, string> = { "Content-Type": "application/json" }
  if (tenantId) {
    h["X-Tenant-ID"] = tenantId
  }
  return h
}

// ── Request / Response types ──────────────────────────────────────────────────

export interface QuickCheckoutItem {
  product_id: string
  warehouse_id: string
  qty: string
  unit_id?: string
  unit_price: string
}

export type PaymentMethod =
  | "cash"
  | "wechat"
  | "alipay"
  | "card"
  | "credit"
  | "transfer"

export interface QuickCheckoutRequest {
  items: QuickCheckoutItem[]
  payment_method: PaymentMethod
  /** String representation to avoid floating-point issues; backend accepts this. */
  paid_amount: string
  customer_name?: string
}

export interface QuickCheckoutResult {
  bill_id: string
  bill_no: string
  total_amount: string
  receivable_amount: string
}

export interface SaleBillSummary {
  id: string
  bill_no: string
  total_amount: string
  paid_amount: string
  payment_method: string
  created_at: string
}

export interface InsufficientStockError {
  error: "insufficient_stock"
  product_id: string
  available: number
  requested: number
}

// ── API functions ─────────────────────────────────────────────────────────────

/**
 * quickCheckout calls POST /api/v1/sale-bills/quick-checkout.
 * Throws an Error with message from the response body on non-2xx status.
 * For 422 insufficient_stock, the error message includes the error code so callers
 * can parse it: `error.message.includes('insufficient_stock')`.
 */
export async function quickCheckout(
  req: QuickCheckoutRequest,
  tenantId?: string
): Promise<QuickCheckoutResult> {
  const res = await fetch(`${BASE}/sale-bills/quick-checkout`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(req),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({})) as Record<string, unknown>
    throw new Error(
      (body.error as string | undefined) ??
        `quickCheckout: HTTP ${res.status}`
    )
  }
  return res.json() as Promise<QuickCheckoutResult>
}

/**
 * listTodaySaleBills fetches today's sale bills (up to 200).
 * Uses today's date in YYYY-MM-DD format for date_from and date_to params.
 */
export async function listTodaySaleBills(
  tenantId?: string
): Promise<SaleBillSummary[]> {
  const today = new Date().toISOString().slice(0, 10)
  const url = new URL(BASE + "/sale-bills", window.location.origin)
  url.searchParams.set("date_from", today)
  url.searchParams.set("date_to", today)
  url.searchParams.set("page_size", "200")

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  if (!res.ok) {
    const body = await res.json().catch(() => ({})) as Record<string, unknown>
    throw new Error(
      (body.error as string | undefined) ??
        `listTodaySaleBills: HTTP ${res.status}`
    )
  }
  const data = await res.json() as { items?: SaleBillSummary[] }
  return data.items ?? []
}
