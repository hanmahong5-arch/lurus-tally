/**
 * API wrapper for payment endpoints (Story 7.1).
 * Covers: record payment, list payments by bill.
 */
import { apiFetch } from "./client"
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

export async function recordPayment(
  body: RecordPaymentRequest,
  tenantId?: string
): Promise<Payment> {
  return apiFetch<Payment>("/payments", { method: "POST", body: JSON.stringify(body), tenantId })
}

export async function listPayments(billId: string, tenantId?: string): Promise<Payment[]> {
  const result = await apiFetch<{ payments: Payment[] } | Payment[]>(
    `/payments?bill_id=${encodeURIComponent(billId)}`,
    { tenantId },
  )
  if (Array.isArray(result)) return result
  return (result as { payments: Payment[] }).payments ?? []
}
