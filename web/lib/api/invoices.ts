/**
 * API wrapper for the invoice registry (登记发票). Amounts are decimal strings;
 * every sum / difference goes through Decimal, never through JS floats.
 */

import Decimal from "decimal.js"
import { apiFetch } from "./client"

export type InvoiceDirection = "out" | "in"
export type InvoiceState = "issued" | "void" | "red"

export const INVOICE_TYPES = ["数电普票", "数电专票", "纸质普票", "纸质专票"] as const

export const DIRECTION_LABEL: Record<InvoiceDirection, string> = { out: "销项", in: "进项" }
export const INVOICE_STATE_LABEL: Record<InvoiceState, string> = {
  issued: "正常",
  void: "已作废",
  red: "已红冲",
}

export interface InvoiceLink {
  bill_id: string
  bill_no: string
  amount: string
}

export interface Invoice {
  id: string
  direction: InvoiceDirection
  invoice_no: string
  invoice_code: string
  invoice_type: string
  partner_id: string
  partner_name: string
  issue_date: string
  amount_excl_tax: string
  tax_amount: string
  total_amount: string
  status: InvoiceState
  red_of_id: string | null
  remark: string
  created_at: string
  updated_at: string
  links: InvoiceLink[]
}

export interface InvoiceListSummary {
  count: number
  amount_excl_tax: string
  tax_amount: string
  total_amount: string
}

export interface InvoiceFilterParams {
  direction?: InvoiceDirection
  status?: InvoiceState
  partner_id?: string
  bill_id?: string
  date_from?: string
  date_to?: string
  q?: string
}

export interface ListInvoicesParams extends InvoiceFilterParams {
  limit?: number
  offset?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export interface CreateInvoiceRequest {
  direction: InvoiceDirection
  invoice_no: string
  invoice_code?: string
  invoice_type: string
  partner_id: string
  issue_date: string
  amount_excl_tax: string
  tax_amount: string
  remark?: string
  links: { bill_id: string; amount: string }[]
}

export interface RedInvoiceRequest {
  invoice_no: string
  invoice_code?: string
  issue_date?: string
  remark?: string
}

export async function listInvoices(
  params: ListInvoicesParams = {}
): Promise<{ items: Invoice[]; total: number; summary?: InvoiceListSummary }> {
  const { limit = 20, offset = 0, tenantId, signal, retry, ...f } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (f.direction) usp.set("direction", f.direction)
  if (f.status) usp.set("status", f.status)
  if (f.partner_id) usp.set("partner_id", f.partner_id)
  if (f.bill_id) usp.set("bill_id", f.bill_id)
  if (f.date_from) usp.set("date_from", f.date_from)
  if (f.date_to) usp.set("date_to", f.date_to)
  if (f.q) usp.set("q", f.q)
  return apiFetch(`/invoices?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getInvoice(id: string, tenantId?: string, signal?: AbortSignal): Promise<Invoice> {
  return apiFetch(`/invoices/${id}`, { tenantId, signal })
}

export async function createInvoice(body: CreateInvoiceRequest, tenantId?: string): Promise<Invoice> {
  return apiFetch("/invoices", { method: "POST", body: JSON.stringify(body), tenantId })
}

export async function voidInvoice(id: string, tenantId?: string): Promise<Invoice> {
  return apiFetch(`/invoices/${id}/void`, { method: "POST", tenantId })
}

/** Registers the red-letter invoice for `id`; the response is the new red-letter invoice. */
export async function redInvoice(id: string, body: RedInvoiceRequest, tenantId?: string): Promise<Invoice> {
  return apiFetch(`/invoices/${id}/red`, { method: "POST", body: JSON.stringify(body), tenantId })
}

/** Parses a user-typed amount; null when blank or not a number. */
export function parseMoney(s: string): Decimal | null {
  const t = s.trim()
  if (!t) return null
  try {
    const d = new Decimal(t)
    return d.isFinite() ? d : null
  } catch {
    return null
  }
}

/** Invoice total = amount excluding tax + tax, as a two-decimal string ("" while the amount is blank). */
export function invoiceTotal(excl: string, tax: string): string {
  const a = parseMoney(excl)
  if (!a) return ""
  const t = parseMoney(tax) ?? new Decimal(0)
  return a.plus(t).toFixed(2)
}

/** What is still to be invoiced on a bill (total - invoiced, never below zero), two decimals. */
export function remainingToInvoice(total: string, invoiced: string | undefined): string {
  const t = parseMoney(total) ?? new Decimal(0)
  const i = parseMoney(invoiced ?? "") ?? new Decimal(0)
  return Decimal.max(t.minus(i), 0).toFixed(2, Decimal.ROUND_DOWN)
}

/** Sum of two-decimal amounts, as a two-decimal string. */
export function sumMoney(values: string[]): string {
  return values.reduce((acc, v) => acc.plus(parseMoney(v) ?? 0), new Decimal(0)).toFixed(2)
}

/** True when `a` is strictly greater than `b` (decimal compare). */
export function moneyGt(a: string, b: string): boolean {
  return (parseMoney(a) ?? new Decimal(0)).gt(parseMoney(b) ?? new Decimal(0))
}
