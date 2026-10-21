/**
 * API wrapper for GET /todo/summary — real counts plus a short preview for
 * every "needs your attention" list (bills to review, low stock, overdue
 * customers, sales not fully invoiced) and the four dashboard figures.
 *
 * `failed` names the sources the backend could not read. Their sections are
 * empty, so callers must show a "partial data" notice instead of an all-clear.
 */
import { apiFetch } from "./client"

const SERVER_BACKEND_URL =
  typeof window === "undefined"
    ? (process.env.BACKEND_URL ?? "http://tally-backend:18200")
    : ""

export interface TodoBill {
  id: string
  bill_no: string
  partner_name: string
  total_amount: string
  bill_date: string
}

export interface TodoStockItem {
  product_id: string
  product_name: string
  available_qty: string
  reorder_point: string
  days_of_supply: string
}

export interface TodoNegativeStockItem {
  product_id: string
  product_name: string
  warehouse_name: string
  /** Negative decimal string, e.g. "-3". */
  on_hand: string
}

export interface TodoDebtor {
  partner_id: string | null
  partner_name: string
  overdue_amount: string
  unpaid_amount: string
  /** YYYY-MM-DD of the oldest open bill; "" when unknown. */
  oldest_bill_date: string
}

export interface TodoSection<T> {
  count: number
  items: T[]
}

export interface TodoKPIs {
  today_sales: string
  /** The three below are dropped by the backend for roles without payment.read. */
  today_collected?: string
  receivable_unpaid?: string
  month_uninvoiced?: string
}

/** Getting-started progress; demo (sample) records never complete a step. */
export interface TodoOnboarding {
  has_product: boolean
  has_purchase: boolean
  has_sale: boolean
  has_payment: boolean
  /** true while sample data is still in the books. */
  has_demo: boolean
}

export interface TodoSummary {
  draft_purchases: TodoSection<TodoBill>
  draft_sales: TodoSection<TodoBill>
  low_stock: TodoSection<TodoStockItem>
  overdue_customers: TodoSection<TodoDebtor>
  uninvoiced_sales: TodoSection<TodoBill>
  /** Offline sales left stock below zero; clears itself once a stocktake brings on-hand back to >= 0. Capped at 50 rows, count is the real total. */
  negative_stock?: TodoSection<TodoNegativeStockItem>
  kpis: TodoKPIs
  total: number
  /** false only for a brand-new tenant with no product and no bill. */
  has_data: boolean
  onboarding?: TodoOnboarding
  /** false when no AI backend is wired; the sidebar then hides the assistant. */
  ai_enabled?: boolean
  /** false when the platform has no cloud receipt printer account; cloud-print controls stay hidden. */
  cloud_print_available?: boolean
  failed: string[]
  generated_at: string
}

/** Source keys the backend may list in `failed`. */
export const TODO_SOURCE_KPIS = "kpis"

/**
 * Server-side fetch. Returns null on any error so the page can say "load
 * failed" rather than pretend nothing is pending.
 */
export async function fetchTodoSummary(accessToken: string): Promise<TodoSummary | null> {
  if (!accessToken) return null
  try {
    const res = await fetch(`${SERVER_BACKEND_URL}/api/v1/todo/summary`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      // Fresh on every visit: the figures must follow what was just recorded or cleared.
      cache: "no-store",
    })
    if (!res.ok) return null
    return (await res.json()) as TodoSummary
  } catch {
    return null
  }
}

/** Browser-side fetch for the sidebar badge; silent so a failure never toasts. */
export async function getTodoSummary(
  opts: { tenantId?: string; signal?: AbortSignal } = {}
): Promise<TodoSummary> {
  return apiFetch<TodoSummary>("/todo/summary", { tenantId: opts.tenantId, signal: opts.signal, silent: true })
}
