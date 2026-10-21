/**
 * API wrapper for lump-sum receipts and partner statements.
 * A receipt settles one payment from a customer (or to a supplier) against the oldest
 * unpaid bills first; the preview shows which bills it would cover before anything is saved.
 */
import { apiFetch } from "./client"

export type ReceiptDirection = "ar" | "ap"

export interface ReceiptAllocation {
  bill_id: string
  bill_no: string
  bill_date: string
  outstanding: string
  applied: string
  remaining: string
}

export interface ReceiptResult {
  /** Everything the partner owed before this receipt. */
  owed: string
  amount: string
  /** Money booked to the bills: always the whole amount. Absent on a preview from an older server. */
  booked?: string
  /** Sub-cent tail of old bills settled in full (booked less their exact remainder); 0 on a preview. */
  diff?: string
  allocations: ReceiptAllocation[]
}

export interface ReceiptRequest {
  direction: ReceiptDirection
  partner_id: string
  amount: string
  payment_method: string
  remark?: string
}

export async function previewReceipt(
  direction: ReceiptDirection,
  partnerId: string,
  amount: string,
  opts: { tenantId?: string; signal?: AbortSignal } = {}
): Promise<ReceiptResult> {
  const usp = new URLSearchParams({ direction, partner_id: partnerId, amount })
  return apiFetch<ReceiptResult>(`/receipts/preview?${usp.toString()}`, {
    tenantId: opts.tenantId,
    signal: opts.signal,
    silent: true,
  })
}

export async function createReceipt(
  body: ReceiptRequest,
  tenantId?: string,
  idempotencyKey?: string
): Promise<ReceiptResult> {
  return apiFetch<ReceiptResult>("/receipts", { method: "POST", body: JSON.stringify(body), tenantId, idempotencyKey })
}

export type StatementLineKind = "bill" | "return" | "payment"

export interface StatementLine {
  date: string
  kind: StatementLineKind
  bill_id?: string
  bill_no: string
  method?: string
  remark?: string
  /** Effect on what is owed: bills add, returns and payments subtract. */
  delta: string
  balance: string
}

export interface Statement {
  direction: ReceiptDirection
  partner_id: string
  partner_name: string
  from: string
  to: string
  opening: string
  billed: string
  returned: string
  paid: string
  adjustment: string
  closing: string
  lines: StatementLine[]
}

export interface StatementParams {
  direction: ReceiptDirection
  from?: string
  to?: string
}

function statementQuery(p: StatementParams, extra?: Record<string, string>): string {
  const usp = new URLSearchParams({ direction: p.direction })
  if (p.from) usp.set("from", p.from)
  if (p.to) usp.set("to", p.to)
  for (const [k, v] of Object.entries(extra ?? {})) usp.set(k, v)
  return usp.toString()
}

export async function getStatement(
  partnerId: string,
  params: StatementParams,
  opts: { tenantId?: string; signal?: AbortSignal } = {}
): Promise<Statement> {
  return apiFetch<Statement>(`/partners/${encodeURIComponent(partnerId)}/statement?${statementQuery(params)}`, {
    tenantId: opts.tenantId,
    signal: opts.signal,
  })
}

/** CSV download link for one partner's statement. */
export function statementCsvUrl(partnerId: string, params: StatementParams): string {
  return `/api/proxy/partners/${encodeURIComponent(partnerId)}/statement?${statementQuery(params, { format: "csv" })}`
}

/** The on-screen statement page for a partner. */
export function statementHref(direction: ReceiptDirection, partnerId: string, from?: string, to?: string): string {
  const usp = new URLSearchParams({ partner: partnerId, direction })
  if (from) usp.set("from", from)
  if (to) usp.set("to", to)
  return `/statement?${usp.toString()}`
}

/** The print page for a partner's statement. */
export function statementPrintHref(direction: ReceiptDirection, partnerId: string, from?: string, to?: string): string {
  return statementHref(direction, partnerId, from, to).replace("/statement?", "/print/statement?")
}
