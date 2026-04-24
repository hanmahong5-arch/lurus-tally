/**
 * API wrapper for sale bill endpoints (Story 7.1).
 * Covers: create draft, approve, cancel, list, get (with payments), quick-checkout.
 */

import { type BillStatus, type BillItem, type BillLineItemInput } from "./purchase"

export type { BillStatus, BillItem, BillLineItemInput }

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

export interface SaleBillDetail {
  head: SaleBillHead
  items: BillItem[]
  payments: Payment[]
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
  customer_name?: string
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
}

export interface ListSaleBillsParams {
  page?: number
  size?: number
  status?: BillStatus
  partner_id?: string
  tenantId?: string
}

const BASE = "/api/v1"

function headers(tenantId?: string): HeadersInit {
  const h: Record<string, string> = { "Content-Type": "application/json" }
  if (tenantId) h["X-Tenant-ID"] = tenantId
  return h
}

async function handleResponse<T>(res: Response, operation: string): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.message ?? body.error ?? `${operation}: HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

export async function createSaleBill(
  body: CreateSaleBillRequest,
  tenantId?: string
): Promise<{ bill_id: string; bill_no: string }> {
  const res = await fetch(`${BASE}/sale-bills`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(body),
  })
  return handleResponse(res, "createSaleBill")
}

export async function updateSaleBill(
  id: string,
  body: UpdateSaleBillRequest,
  tenantId?: string
): Promise<SaleBillHead> {
  const res = await fetch(`${BASE}/sale-bills/${id}`, {
    method: "PUT",
    headers: headers(tenantId),
    body: JSON.stringify(body),
  })
  return handleResponse(res, "updateSaleBill")
}

export async function approveSaleBill(
  id: string,
  body: ApproveSaleRequest = {},
  tenantId?: string
): Promise<SaleBillHead> {
  const res = await fetch(`${BASE}/sale-bills/${id}/approve`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(body),
  })
  return handleResponse(res, "approveSaleBill")
}

export async function cancelSaleBill(
  id: string,
  tenantId?: string
): Promise<void> {
  const res = await fetch(`${BASE}/sale-bills/${id}/cancel`, {
    method: "POST",
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.message ?? body.error ?? `cancelSaleBill: HTTP ${res.status}`)
  }
}

export async function listSaleBills(
  params: ListSaleBillsParams = {}
): Promise<{ items: SaleBillHead[]; total: number }> {
  const { page = 1, size = 20, status, partner_id, tenantId } = params
  const url = new URL(BASE + "/sale-bills", window.location.origin)
  url.searchParams.set("page", String(page))
  url.searchParams.set("size", String(size))
  if (status !== undefined) url.searchParams.set("status", String(status))
  if (partner_id) url.searchParams.set("partner_id", partner_id)

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  return handleResponse(res, "listSaleBills")
}

export async function getSaleBill(
  id: string,
  tenantId?: string
): Promise<SaleBillDetail> {
  const res = await fetch(`${BASE}/sale-bills/${id}`, {
    headers: headers(tenantId),
  })
  return handleResponse(res, "getSaleBill")
}

export async function quickCheckout(
  body: QuickCheckoutRequest,
  tenantId?: string
): Promise<QuickCheckoutResult> {
  const res = await fetch(`${BASE}/sale-bills/quick-checkout`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(body),
  })
  return handleResponse(res, "quickCheckout")
}
