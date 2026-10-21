/**
 * API wrapper for purchase bill endpoints.
 * Types: BillHead, BillDetail, BillItem, CreatePurchaseBillRequest, etc.
 * Story 7.1 may extract shared types to web/lib/api/bill.ts.
 */
import { apiFetch } from "./client"

export type BillStatus = 0 | 2 | 9 // 0=draft, 2=approved, 9=cancelled

export const BILL_STATUS_LABEL: Record<BillStatus, string> = {
  0: "草稿",
  2: "已审核",
  9: "已取消",
}

/** Per-bill settlement state shown as a small tag on list rows. */
export type PaymentStatus = "unpaid" | "partial" | "paid"

/** Filter value for the payment dropdown: the three states plus "open" (= unpaid + partial). */
export type PaymentFilter = PaymentStatus | "open"

/** Invoice coverage of an approved bill, from live invoice links. */
export type InvoiceStatus = "none" | "partial" | "full"

/** Totals over the whole filtered set (not just the current page); amounts stay decimal strings. */
export interface BillListSummary {
  count: number
  total_amount: string
  paid_amount: string
  unpaid_amount: string
}

export interface BillFilterParams {
  status?: BillStatus
  partner_id?: string
  date_from?: string
  date_to?: string
  payment_status?: PaymentFilter
  invoice_status?: InvoiceStatus
  q?: string
  mine?: boolean
}

export interface BillItem {
  id: string
  tenant_id: string
  head_id: string
  product_id: string
  unit_id?: string
  unit_name?: string
  line_no: number
  qty: string
  unit_price: string
  line_amount: string
  remark?: string
}

export interface BillHead {
  id: string
  tenant_id: string
  bill_no: string
  bill_type: string
  sub_type: string
  status: BillStatus
  partner_id?: string
  warehouse_id?: string
  creator_id: string
  bill_date: string
  subtotal: string
  shipping_fee: string
  tax_amount: string
  total_amount: string
  /** Paid so far (purchase list responses; absent on older payloads). */
  paid_amount?: string
  /** Counterparty name resolved by the list / detail endpoint. */
  partner_name?: string
  /** Warehouse name resolved by the detail endpoint. */
  warehouse_name?: string
  payment_status?: PaymentStatus
  /** Still owed on this bill; string decimal. */
  unpaid_amount?: string
  /** Invoiced so far / coverage (list responses); string decimal. */
  invoiced_amount?: string
  invoice_status?: InvoiceStatus
  approved_at?: string
  approved_by?: string
  remark?: string
  created_at: string
  updated_at: string
}

/** A purchase line plus how much of it has already gone back to the supplier (detail only). */
export type PurchaseBillItem = BillItem & {
  /** In the unit the line was bought in; string decimal. */
  returned_qty?: string
}

export interface BillDetail {
  head: BillHead
  items: PurchaseBillItem[]
}

export interface ReturnPurchaseRequest {
  items: { bill_item_id: string; qty: string }[]
  remark?: string
}

export interface ReturnPurchaseResult {
  bill_id: string
  bill_no: string
  /** Value of the goods sent back; string decimal. */
  return_amount: string
  /** The part of it that cancelled what was still unpaid on the purchase. */
  payable_reduced: string
  /** The rest: already paid for goods that went back, owed to us by the supplier. */
  supplier_owes: string
}

export interface BillLineItemInput {
  product_id: string
  unit_id?: string
  unit_name?: string
  line_no: number
  qty: string
  unit_price: string
  /** Per-line source warehouse; quick checkout reads the warehouse from here. */
  warehouse_id?: string
}

export interface CreatePurchaseBillRequest {
  partner_id?: string
  warehouse_id?: string
  bill_date?: string
  shipping_fee?: string
  tax_amount?: string
  remark?: string
  items: BillLineItemInput[]
  // Multi-currency fields (Story 9.1, cross_border profile)
  currency?: string
  exchange_rate?: string
}

export type UpdatePurchaseBillRequest = CreatePurchaseBillRequest

export interface ListPurchaseBillsParams extends BillFilterParams {
  page?: number
  size?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export function appendBillFilters(usp: URLSearchParams, f: BillFilterParams): void {
  if (f.status !== undefined) usp.set("status", String(f.status))
  if (f.partner_id) usp.set("partner_id", f.partner_id)
  if (f.date_from) usp.set("date_from", f.date_from)
  if (f.date_to) usp.set("date_to", f.date_to)
  if (f.payment_status) usp.set("payment_status", f.payment_status)
  if (f.invoice_status) usp.set("invoice_status", f.invoice_status)
  if (f.q) usp.set("q", f.q)
  if (f.mine) usp.set("mine", "1")
}

/** CSV download link for the bills matching the current list filters. */
export function billsExportUrl(type: "sale" | "purchase", filters: BillFilterParams = {}): string {
  const usp = new URLSearchParams({ type })
  appendBillFilters(usp, filters)
  return `/api/proxy/exports/bills.csv?${usp.toString()}`
}

export async function createPurchaseBill(
  body: CreatePurchaseBillRequest,
  tenantId?: string,
  idempotencyKey?: string
): Promise<{ bill_id: string; bill_no: string }> {
  return apiFetch("/purchase-bills", { method: "POST", body: JSON.stringify(body), tenantId, idempotencyKey })
}

export async function updatePurchaseBill(
  id: string,
  body: UpdatePurchaseBillRequest,
  tenantId?: string
): Promise<BillHead> {
  return apiFetch(`/purchase-bills/${id}`, { method: "PUT", body: JSON.stringify(body), tenantId })
}

/** Optional payment made together with the approval; both omitted = approve only. */
export interface ApprovePurchaseRequest {
  paid_amount?: string
  payment_method?: string
  /** Confirms lines with a zero unit price are gifts, booked at zero cost. */
  allow_zero_price?: boolean
}

export async function approvePurchaseBill(
  id: string,
  tenantId?: string,
  body?: ApprovePurchaseRequest
): Promise<BillHead> {
  return apiFetch(`/purchase-bills/${id}/approve`, {
    method: "POST",
    tenantId,
    ...(body ? { body: JSON.stringify(body) } : {}),
  })
}

export async function cancelPurchaseBill(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/purchase-bills/${id}/cancel`, { method: "POST", tenantId })
}

export async function listPurchaseBills(
  params: ListPurchaseBillsParams = {}
): Promise<{ items: BillHead[]; total: number; summary?: BillListSummary }> {
  const { page = 1, size = 20, tenantId, signal, retry, ...filters } = params
  const usp = new URLSearchParams({ page: String(page), size: String(size) })
  appendBillFilters(usp, filters)
  return apiFetch(`/purchase-bills?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getPurchaseBill(id: string, tenantId?: string, signal?: AbortSignal): Promise<BillDetail> {
  return apiFetch(`/purchase-bills/${id}`, { tenantId, signal })
}

export async function restorePurchaseBill(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/purchase-bills/${id}/restore`, { method: "POST", tenantId })
}

/** Sends goods of an approved purchase back to the supplier: stock goes out and the payable drops. */
export async function returnPurchaseBill(
  id: string,
  body: ReturnPurchaseRequest,
  tenantId?: string,
  idempotencyKey?: string
): Promise<ReturnPurchaseResult> {
  return apiFetch(`/purchase-bills/${id}/return`, {
    method: "POST",
    body: JSON.stringify(body),
    tenantId,
    idempotencyKey,
  })
}
