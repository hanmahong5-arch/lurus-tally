/**
 * API wrapper for GET /receivables — open balances per customer / supplier,
 * split by how long the bill has been open. Only approved bills count.
 */
import { apiFetch } from "./client"
import { appendBillFilters } from "./purchase"
import type { BillFilterParams, BillListSummary } from "./purchase"

export type ReceivableDirection = "ar" | "ap"

export interface PartnerReceivable {
  /** null for bills without a customer (walk-in sales). */
  partner_id: string | null
  partner_name: string
  unpaid: string
  b0_30: string
  b31_60: string
  b61_90: string
  b90p: string
  /** YYYY-MM-DD of the oldest open bill; "" when none. */
  oldest_bill_date: string
}

export interface ReceivablesResult {
  direction: ReceivableDirection
  as_of: string
  items: PartnerReceivable[]
  total: PartnerReceivable
}

export async function getReceivables(
  direction: ReceivableDirection,
  opts: { tenantId?: string; signal?: AbortSignal; retry?: number; asOf?: string } = {}
): Promise<ReceivablesResult> {
  const { tenantId, signal, retry, asOf } = opts
  const q = asOf ? `&as_of=${encodeURIComponent(asOf)}` : ""
  return apiFetch<ReceivablesResult>(`/receivables?direction=${direction}${q}`, { tenantId, signal, retry })
}

/** partner_id -> unpaid amount, for the 欠款 column on the customer / supplier pages. */
export function unpaidByPartner(r: ReceivablesResult): Map<string, string> {
  const m = new Map<string, string>()
  for (const it of r.items) if (it.partner_id) m.set(it.partner_id, it.unpaid)
  return m
}

export interface OpenBillsParams extends BillFilterParams {
  page?: number
  size?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

/** Sale / purchase bills listed oldest first, so the longest-standing debt comes first. */
export async function listBillsOldestFirst<T>(
  kind: "sale" | "purchase",
  params: OpenBillsParams = {}
): Promise<{ items: T[]; total: number; summary?: BillListSummary }> {
  const { page = 1, size = 20, tenantId, signal, retry, ...filters } = params
  const usp = new URLSearchParams({ page: String(page), size: String(size), sort: "bill_date_asc" })
  appendBillFilters(usp, filters)
  const path = kind === "sale" ? "/sale-bills" : "/purchase-bills"
  return apiFetch(`${path}?${usp.toString()}`, { tenantId, signal, retry })
}

/** Link from a customer / supplier row to that partner's open bills. */
export function openBillsHref(direction: ReceivableDirection, partnerId: string, partnerName: string): string {
  const usp = new URLSearchParams({ partner: partnerId, pname: partnerName })
  if (direction === "ap") usp.set("tab", "purchases")
  return `/payments?${usp.toString()}`
}
