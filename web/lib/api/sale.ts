/**
 * API wrapper for sale bill endpoints (Story 7.1).
 * Covers: create draft, approve, cancel, list, get (with payments), quick-checkout.
 */

import { apiFetch } from "./client"
import {
  type BillStatus,
  type BillItem,
  type BillLineItemInput,
  type BillFilterParams,
  type BillListSummary,
  type PaymentStatus,
  type PaymentFilter,
  type InvoiceStatus,
  billsExportUrl,
  appendBillFilters,
} from "./purchase"

export type { BillStatus, BillItem, BillLineItemInput, BillFilterParams, BillListSummary, PaymentStatus, PaymentFilter, InvoiceStatus }
export { billsExportUrl }

export interface SaleBillHead {
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
  paid_amount: string
  receivable_amount: string
  /** Counterparty name resolved by the list / detail endpoint. */
  partner_name?: string
  /** Warehouse name resolved by the detail endpoint. */
  warehouse_name?: string
  payment_status?: PaymentStatus
  /** Still to collect on this bill; string decimal. */
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

export interface Payment {
  id: string
  bill_id: string
  amount: string
  pay_type: string
  pay_date: string
  remark?: string
}

/** A sale line plus how much of it the customer has already given back (sale detail only). */
export type SaleBillItem = BillItem & {
  /** In the unit the line was sold in; string decimal. */
  returned_qty?: string
}

export interface SaleBillDetail {
  head: SaleBillHead
  items: SaleBillItem[]
  payments: Payment[]
  /** Still to collect after returns; string decimal. Top level, not on head. */
  receivable_amount?: string
  /** Value of the goods already returned; string decimal. */
  returned_amount?: string
  /** Paid beyond the post-return total: owed back to the customer, not paid out by the system. */
  refund_due_amount?: string
}

export interface ReturnSaleRequest {
  items: { bill_item_id: string; qty: string }[]
  /** Defaults to the warehouse the sale shipped from. */
  warehouse_id?: string
  remark?: string
  /** Money handed back with the return (string decimal); omit for a return without refund. */
  refund_amount?: string
  /** How the refund is paid (cash / wechat / alipay / card / transfer); defaults to cash. */
  refund_method?: string
  /** Book only the refund of a return that already went through; no goods move. */
  refund_only?: boolean
  /** With refund_only: the return (bill_id of its result) the refund belongs to. */
  return_bill_id?: string
}

export interface ReturnSaleResult {
  bill_id: string
  bill_no: string
  return_amount: string
  /** Refund booked with the return; absent when none was asked for. */
  refunded_amount?: string
  /** Set when the goods went back but the refund could not be booked. */
  refund_error?: string
}

export interface CreateSaleBillRequest {
  partner_id?: string
  warehouse_id?: string
  bill_date?: string
  remark?: string
  items: BillLineItemInput[]
  // Multi-currency fields (Story 9.1, cross_border profile)
  currency?: string
  exchange_rate?: string
}

export type UpdateSaleBillRequest = CreateSaleBillRequest

export interface ApproveSaleRequest {
  paid_amount?: string
  payment_method?: string
}

export interface QuickCheckoutRequest {
  /** Customer profile id; wins over customer_name when both are sent. */
  partner_id?: string
  customer_name?: string
  warehouse_id?: string
  items: BillLineItemInput[]
  payment_method: string
  paid_amount: string
  remark?: string
}

export interface QuickCheckoutResult {
  bill_id: string
  bill_no: string
  total_amount: string
  receivable_amount: string
  /** Cash handed over beyond the total; not counted as payment. */
  change_amount?: string
}

export interface ListSaleBillsParams extends BillFilterParams {
  page?: number
  size?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export async function createSaleBill(
  body: CreateSaleBillRequest,
  tenantId?: string,
  idempotencyKey?: string
): Promise<{ bill_id: string; bill_no: string }> {
  return apiFetch("/sale-bills", { method: "POST", body: JSON.stringify(body), tenantId, idempotencyKey })
}

export async function updateSaleBill(
  id: string,
  body: UpdateSaleBillRequest,
  tenantId?: string
): Promise<SaleBillHead> {
  return apiFetch(`/sale-bills/${id}`, { method: "PUT", body: JSON.stringify(body), tenantId })
}

export async function approveSaleBill(
  id: string,
  body: ApproveSaleRequest = {},
  tenantId?: string
): Promise<SaleBillHead> {
  return apiFetch(`/sale-bills/${id}/approve`, {
    method: "POST",
    body: JSON.stringify(body),
    tenantId,
  })
}

/** Gives goods of an approved sale back: stock returns and the amount owed drops. */
export async function returnSaleBill(
  id: string,
  body: ReturnSaleRequest,
  tenantId?: string,
  idempotencyKey?: string
): Promise<ReturnSaleResult> {
  return apiFetch(`/sale-bills/${id}/return`, { method: "POST", body: JSON.stringify(body), tenantId, idempotencyKey })
}

export async function cancelSaleBill(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/sale-bills/${id}/cancel`, { method: "POST", tenantId })
}

export async function listSaleBills(
  params: ListSaleBillsParams = {}
): Promise<{ items: SaleBillHead[]; total: number; summary?: BillListSummary }> {
  const { page = 1, size = 20, tenantId, signal, retry, ...filters } = params
  const usp = new URLSearchParams({ page: String(page), size: String(size) })
  appendBillFilters(usp, filters)
  return apiFetch(`/sale-bills?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getSaleBill(id: string, tenantId?: string, signal?: AbortSignal): Promise<SaleBillDetail> {
  return apiFetch(`/sale-bills/${id}`, { tenantId, signal })
}

export async function quickCheckout(
  body: QuickCheckoutRequest,
  tenantId?: string,
  idempotencyKey?: string
): Promise<QuickCheckoutResult> {
  return apiFetch("/sale-bills/quick-checkout", {
    method: "POST",
    body: JSON.stringify(body),
    tenantId,
    idempotencyKey,
  })
}

export async function restoreSaleBill(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/sale-bills/${id}/restore`, { method: "POST", tenantId })
}
