/**
 * API wrapper for payment endpoints (Story 7.1).
 * Covers: record payment, list payments by bill.
 */

import type { Payment } from "./sale"

export type { Payment }

export const PAY_TYPE_LABEL: Record<string, string> = {
  cash: "现金",
  wechat: "微信",
  alipay: "支付宝",
  card: "银行卡",
  credit: "赊账",
  transfer: "转账",
}

export interface RecordPaymentRequest {
  bill_id: string
  amount: string
  payment_method: string
  remark?: string
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

export async function recordPayment(
  body: RecordPaymentRequest,
  tenantId?: string
): Promise<Payment> {
  const res = await fetch(`${BASE}/payments`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(body),
  })
  return handleResponse(res, "recordPayment")
}

export async function listPayments(
  billId: string,
  tenantId?: string
): Promise<Payment[]> {
  const url = new URL(BASE + "/payments", window.location.origin)
  url.searchParams.set("bill_id", billId)
  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  const result = await handleResponse<{ payments: Payment[] } | Payment[]>(res, "listPayments")
  // Handle both array response and wrapped response
  if (Array.isArray(result)) return result
  return (result as { payments: Payment[] }).payments ?? []
}
