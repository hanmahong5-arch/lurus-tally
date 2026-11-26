import { apiFetch } from "@/lib/api/client"

export interface MoneyCandidate {
  bill_id: string
  bill_no: string
  partner_name: string
  outstanding: string
  direction: string
}

export interface MoneyLine {
  id: string
  channel: string
  booked_at: string
  amount: string
  counterparty: string
  memo: string
  external_id: string
  candidates: MoneyCandidate[]
  suggested: boolean
}

export function listMoneyLines(signal?: AbortSignal) {
  return apiFetch<{ items: MoneyLine[] }>("/money-lines", { silent: true, signal })
}

export function importMoneyFile(fileBase64: string, accountRef: string) {
  return apiFetch<{ channel: string; inserted: number; skipped: number }>("/money-lines", {
    method: "POST",
    body: JSON.stringify({ file_base64: fileBase64, account_ref: accountRef }),
  })
}

export function matchMoneyLine(id: string, billId: string) {
  return apiFetch<{ ok: boolean }>(`/money-lines/${id}/match`, {
    method: "POST",
    body: JSON.stringify({ bill_id: billId }),
  })
}

export function ignoreMoneyLine(id: string) {
  return apiFetch<{ ok: boolean }>(`/money-lines/${id}/ignore`, { method: "POST" })
}
