/**
 * API wrapper for POS-related endpoints.
 * Amounts are represented as strings to avoid JSON floating-point loss.
 * Story 2.1 TODO: replace tenantId param with session-cookie auth.
 */
import { apiFetch } from "./client"

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
 * For 422 insufficient_stock the thrown ApiError carries body.error so callers
 * can branch on `err.body.error === 'insufficient_stock'` or `err.message.includes(...)`.
 */
export async function quickCheckout(
  req: QuickCheckoutRequest,
  tenantId?: string
): Promise<QuickCheckoutResult> {
  return apiFetch<QuickCheckoutResult>("/sale-bills/quick-checkout", {
    method: "POST",
    body: JSON.stringify(req),
    tenantId,
  })
}

/**
 * listTodaySaleBills fetches today's sale bills (up to 200).
 * Uses today's date in YYYY-MM-DD format for date_from and date_to params.
 */
export async function listTodaySaleBills(
  tenantId?: string,
  signal?: AbortSignal
): Promise<SaleBillSummary[]> {
  const today = new Date().toISOString().slice(0, 10)
  const qs = `?date_from=${today}&date_to=${today}&page_size=200`
  const data = await apiFetch<{ items?: SaleBillSummary[] }>("/sale-bills" + qs, { tenantId, signal })
  return data.items ?? []
}
