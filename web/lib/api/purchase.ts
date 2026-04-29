/**
 * API wrapper for purchase bill endpoints.
 * Types: BillHead, BillDetail, BillItem, CreatePurchaseBillRequest, etc.
 * Story 7.1 may extract shared types to web/lib/api/bill.ts.
 */

export type BillStatus = 0 | 2 | 9 // 0=draft, 2=approved, 9=cancelled

export const BILL_STATUS_LABEL: Record<BillStatus, string> = {
  0: "草稿",
  2: "已审核",
  9: "已取消",
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
  approved_at?: string
  approved_by?: string
  remark?: string
  created_at: string
  updated_at: string
}

export interface BillDetail {
  head: BillHead
  items: BillItem[]
}

export interface BillLineItemInput {
  product_id: string
  unit_id?: string
  unit_name?: string
  line_no: number
  qty: string
  unit_price: string
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

export interface ListPurchaseBillsParams {
  page?: number
  size?: number
  status?: BillStatus
  partner_id?: string
  tenantId?: string
}

const BASE = "/api/proxy"

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

export async function createPurchaseBill(
  body: CreatePurchaseBillRequest,
  tenantId?: string
): Promise<{ bill_id: string; bill_no: string }> {
  const res = await fetch(`${BASE}/purchase-bills`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(body),
  })
  return handleResponse(res, "createPurchaseBill")
}

export async function updatePurchaseBill(
  id: string,
  body: UpdatePurchaseBillRequest,
  tenantId?: string
): Promise<BillHead> {
  const res = await fetch(`${BASE}/purchase-bills/${id}`, {
    method: "PUT",
    headers: headers(tenantId),
    body: JSON.stringify(body),
  })
  return handleResponse(res, "updatePurchaseBill")
}

export async function approvePurchaseBill(
  id: string,
  tenantId?: string
): Promise<BillHead> {
  const res = await fetch(`${BASE}/purchase-bills/${id}/approve`, {
    method: "POST",
    headers: headers(tenantId),
  })
  return handleResponse(res, "approvePurchaseBill")
}

export async function cancelPurchaseBill(
  id: string,
  tenantId?: string
): Promise<void> {
  const res = await fetch(`${BASE}/purchase-bills/${id}/cancel`, {
    method: "POST",
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.message ?? body.error ?? `cancelPurchaseBill: HTTP ${res.status}`)
  }
}

export async function listPurchaseBills(
  params: ListPurchaseBillsParams = {}
): Promise<{ items: BillHead[]; total: number }> {
  const { page = 1, size = 20, status, partner_id, tenantId } = params
  const url = new URL(BASE + "/purchase-bills", window.location.origin)
  url.searchParams.set("page", String(page))
  url.searchParams.set("size", String(size))
  if (status !== undefined) url.searchParams.set("status", String(status))
  if (partner_id) url.searchParams.set("partner_id", partner_id)

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  return handleResponse(res, "listPurchaseBills")
}

export async function getPurchaseBill(
  id: string,
  tenantId?: string
): Promise<BillDetail> {
  const res = await fetch(`${BASE}/purchase-bills/${id}`, {
    headers: headers(tenantId),
  })
  return handleResponse(res, "getPurchaseBill")
}
