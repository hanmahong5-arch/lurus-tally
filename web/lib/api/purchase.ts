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
  signal?: AbortSignal
  retry?: number
}

export async function createPurchaseBill(
  body: CreatePurchaseBillRequest,
  tenantId?: string
): Promise<{ bill_id: string; bill_no: string }> {
  return apiFetch("/purchase-bills", { method: "POST", body: JSON.stringify(body), tenantId })
}

export async function updatePurchaseBill(
  id: string,
  body: UpdatePurchaseBillRequest,
  tenantId?: string
): Promise<BillHead> {
  return apiFetch(`/purchase-bills/${id}`, { method: "PUT", body: JSON.stringify(body), tenantId })
}

export async function approvePurchaseBill(id: string, tenantId?: string): Promise<BillHead> {
  return apiFetch(`/purchase-bills/${id}/approve`, { method: "POST", tenantId })
}

export async function cancelPurchaseBill(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/purchase-bills/${id}/cancel`, { method: "POST", tenantId })
}

export async function listPurchaseBills(
  params: ListPurchaseBillsParams = {}
): Promise<{ items: BillHead[]; total: number }> {
  const { page = 1, size = 20, status, partner_id, tenantId, signal, retry } = params
  const usp = new URLSearchParams({ page: String(page), size: String(size) })
  if (status !== undefined) usp.set("status", String(status))
  if (partner_id) usp.set("partner_id", partner_id)
  return apiFetch(`/purchase-bills?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getPurchaseBill(id: string, tenantId?: string, signal?: AbortSignal): Promise<BillDetail> {
  return apiFetch(`/purchase-bills/${id}`, { tenantId, signal })
}

export async function restorePurchaseBill(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/purchase-bills/${id}/restore`, { method: "POST", tenantId })
}
