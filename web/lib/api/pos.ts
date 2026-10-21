/**
 * API wrapper for POS-related endpoints.
 * Amounts are represented as strings to avoid JSON floating-point loss.
 * Story 2.1 TODO: replace tenantId param with session-cookie auth.
 */
import { apiFetch } from "./client"

// ── Request / Response types ──────────────────────────────────────────────────

export interface QuickCheckoutItem {
  product_id: string
  /** Omit to use the tenant default warehouse (resolved by the backend). */
  warehouse_id?: string
  qty: string
  unit_id?: string
  /** Display name printed on the receipt / bill detail. */
  unit_name?: string
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
  /** Customer profile id; credit sales must carry one so the debt shows on the customer. */
  partner_id?: string
  customer_name?: string
  /** Cashier's free note from the cart; kept on the bill whatever the payment method. */
  remark?: string
  /** Whole-bill discount (NUMERIC string, deducted from the line total); omit for none. */
  discount_amount?: string
  /** Device-unique checkout reference (<device>-<seq>, at most 64); the server dedupes on it per tenant. */
  client_ref?: string
  /** "pos_offline" marks a checkout that was queued locally and uploaded later. */
  source?: "pos_offline"
  /** When the sale really happened (RFC3339), kept on the bill when it is uploaded late. */
  occurred_at?: string
}

export interface QuickCheckoutResult {
  bill_id: string
  bill_no: string
  total_amount: string
  receivable_amount: string
  change_amount?: string
}

export interface SaleBillSummary {
  id: string
  bill_no: string
  total_amount: string
  paid_amount: string
  /** Still owed, net of returns (can go negative when a return refunds more than was owed). */
  unpaid_amount: string
  created_at: string
}

export interface InsufficientStockError {
  error: "insufficient_stock"
  product_id: string
  /** Decimal string — backend uses shopspring/decimal to preserve precision. */
  available: string
  /** Decimal string — backend uses shopspring/decimal to preserve precision. */
  requested: string
}

// ── API functions ─────────────────────────────────────────────────────────────

/**
 * quickCheckout calls POST /api/v1/sale-bills/quick-checkout.
 * For 422 insufficient_stock the thrown ApiError carries body.error so callers
 * can branch on `err.body.error === 'insufficient_stock'` or `err.message.includes(...)`.
 */
export async function quickCheckout(
  req: QuickCheckoutRequest,
  tenantId?: string,
  idempotencyKey?: string
): Promise<QuickCheckoutResult> {
  return apiFetch<QuickCheckoutResult>("/sale-bills/quick-checkout", {
    method: "POST",
    body: JSON.stringify(req),
    tenantId,
    idempotencyKey,
  })
}

/** Today's calendar date in Beijing time (YYYY-MM-DD), whatever the device time zone is. */
export function beijingToday(now: Date = new Date()): string {
  return new Date(now.getTime() + 8 * 3600 * 1000).toISOString().slice(0, 10)
}

export interface PosPayTypeTotal {
  pay_type: string
  /** Number of bills that took this payment method. */
  count: number
  amount: string
}

/** One person's slice of the day; key is an opaque parameter for the cashier filter, never shown. */
export interface PosCashier {
  key: string
  name: string
  count: number
  total_amount: string
  paid_amount: string
  refund_amount: string
}

/** Shift-end totals from GET /sale-bills/pos-summary (gross of returns). */
export interface PosSummary {
  date: string
  count: number
  total_amount: string
  paid_amount: string
  /** Unpaid remainder of the day's bills (on-credit sales). */
  credit_amount: string
  by_pay_type: PosPayTypeTotal[]
  return_count: number
  return_amount: string
  /** Refund payments paid out that day, whatever day the sale was written. */
  refund_count: number
  /** Total of those refunds, as a positive amount. */
  refund_amount: string
  /** Per-person split; only the shop-wide answer for owner / manager carries it. */
  cashiers?: PosCashier[]
}

/**
 * getPosSummary fetches the shift summary for one Beijing day (default today).
 * mine narrows it to the signed-in person's own bills, payments and returns; cashier (a key from
 * the summary's cashiers) narrows it to that person, honoured for owner and manager only.
 */
export async function getPosSummary(
  date?: string,
  tenantId?: string,
  signal?: AbortSignal,
  mine?: boolean,
  cashier?: string
): Promise<PosSummary> {
  const params = new URLSearchParams()
  if (date) params.set("date", date)
  if (mine) params.set("mine", "1")
  if (cashier) params.set("cashier", cashier)
  const qs = params.size > 0 ? `?${params.toString()}` : ""
  return apiFetch<PosSummary>("/sale-bills/pos-summary" + qs, { tenantId, signal })
}

/**
 * listTodaySaleBills fetches today's approved sale bills (up to 200).
 * Uses today's Beijing date in YYYY-MM-DD format for date_from and date_to params.
 */
export async function listTodaySaleBills(
  tenantId?: string,
  signal?: AbortSignal,
  mine?: boolean
): Promise<SaleBillSummary[]> {
  const today = beijingToday()
  const qs = `?date_from=${today}&date_to=${today}&status=2&size=200${mine ? "&mine=1" : ""}`
  const data = await apiFetch<{ items?: SaleBillSummary[] }>("/sale-bills" + qs, { tenantId, signal })
  return data.items ?? []
}
