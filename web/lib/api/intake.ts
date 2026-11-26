import { apiFetch } from "@/lib/api/client"
import { ApiError } from "@/lib/api/errors"

export interface IntakeLine {
  name?: string
  product_id?: string
  qty?: string
  unit_price?: string
}

export interface IntakeDoc {
  id: string
  source: string
  direction: string
  status: string
  invoice_no?: string
  issue_date?: string
  parsed: {
    invoice_no?: string
    invoice_code?: string
    invoice_type?: string
    seller_name?: string
    seller_tax_no?: string
    partner_id?: string
    partner_name?: string
    issue_date?: string
    amount_excl_tax?: string
    tax_amount?: string
    total_amount?: string
    money_ok?: boolean
    transcript?: string
    lines?: IntakeLine[]
    matches?: { bill_id: string; bill_no: string; amount?: string }[]
    hints?: string[]
  }
  invoice_id?: string
  draft_bill_no?: string
  reject_reason?: string
  open_bills?: { id: string; bill_no: string; bill_date?: string; status: number; total?: string; remaining?: string }[]
  has_file?: boolean
  content_type?: string
}

export interface CapturePayload {
  source: string
  direction?: string
  client_ref: string
  qr_payload?: string
  transcript?: string
  content_type?: string
  file_base64?: string
}

export function listIntake(signal?: AbortSignal) {
  return apiFetch<{ items: IntakeDoc[]; total: number }>("/intake?limit=100", { silent: true, signal })
}

export function getIntake(id: string, opts?: { partnerId?: string; direction?: string; choosePartner?: boolean }, signal?: AbortSignal) {
  const q = new URLSearchParams()
  if (opts?.choosePartner) q.set("partner_id", opts.partnerId ?? "")
  else if (opts?.partnerId) q.set("partner_id", opts.partnerId)
  if (opts?.direction) q.set("direction", opts.direction)
  const suffix = q.toString()
  return apiFetch<IntakeDoc>(`/intake/${id}${suffix ? `?${suffix}` : ""}`, { silent: true, signal })
}

export function intakeCount(signal?: AbortSignal) {
  return apiFetch<{ count: number }>("/intake/count", { silent: true, signal }).then((r) => r.count)
}

export function captureIntake(body: CapturePayload, idempotencyKey: string) {
  return apiFetch<IntakeDoc>("/intake", {
    method: "POST",
    body: JSON.stringify(body),
    idempotencyKey,
    silent: true,
  })
}

export function lockIntake(id: string) {
  return apiFetch<{ ok: boolean }>(`/intake/${id}/lock`, { method: "POST", silent: true })
}

export function acceptIntake(id: string, body: unknown, idempotencyKey: string) {
  return apiFetch<{ document: IntakeDoc; warning?: string; draft_bill_no?: string }>(`/intake/${id}/accept`, {
    method: "POST",
    body: JSON.stringify(body),
    idempotencyKey,
  })
}

export async function intakeFile(id: string, signal?: AbortSignal): Promise<{ blob: Blob; type: string }> {
  const res = await fetch(`/api/proxy/intake/${id}/file`, { signal, cache: "no-store" })
  if (!res.ok) throw new ApiError(res.status, "intake_file", "原图没有打开", {})
  return { blob: await res.blob(), type: res.headers.get("content-type") || "" }
}

export function rejectIntake(id: string, reason: string) {
  return apiFetch<IntakeDoc>(`/intake/${id}/reject`, {
    method: "POST",
    body: JSON.stringify({ reason }),
    silent: true,
  })
}
