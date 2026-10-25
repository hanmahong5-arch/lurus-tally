/**
 * API wrapper for the customer endpoints. Customers live in tally.partner
 * (partner_type customer / both / member); same fetch pattern as suppliers.ts.
 */
import { apiFetch } from "./client"
import { ApiError } from "./errors"
import { fetchPartnerTemplate, uploadPartnerCsv } from "./partner-import"

export interface CustomerItem {
  id: string
  tenant_id: string
  name: string
  code?: string
  contact_name?: string
  mobile?: string
  phone?: string
  address?: string
  remark?: string
  created_at: string
  updated_at: string
}

export interface CustomerListParams {
  q?: string
  limit?: number
  offset?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export interface CustomerListResult {
  items: CustomerItem[]
  total: number
}

export type CustomerCreateInput = {
  name: string
  code?: string
  contact_name?: string
  mobile?: string
  phone?: string
  address?: string
  remark?: string
}

export async function listCustomers(
  params: CustomerListParams = {}
): Promise<CustomerListResult> {
  const { q, limit = 20, offset = 0, tenantId, signal, retry } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  return apiFetch<CustomerListResult>(`/customers?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getCustomer(id: string, tenantId?: string): Promise<CustomerItem> {
  return apiFetch<CustomerItem>(`/customers/${id}`, { tenantId })
}

export async function createCustomer(
  input: CustomerCreateInput,
  tenantId?: string
): Promise<CustomerItem> {
  return apiFetch<CustomerItem>("/customers", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export type CustomerUpdateInput = Partial<CustomerCreateInput>

export async function updateCustomer(
  id: string,
  input: CustomerUpdateInput,
  tenantId?: string
): Promise<CustomerItem> {
  return apiFetch<CustomerItem>(`/customers/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteCustomer(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/customers/${id}`, { method: "DELETE", tenantId })
}

export async function restoreCustomer(id: string, tenantId?: string): Promise<CustomerItem> {
  return apiFetch<CustomerItem>(`/customers/${id}/restore`, { method: "POST", tenantId })
}

export interface CustomerDuplicate {
  /** Existing customer that already owns the submitted name / mobile. */
  existingId: string
  message: string
}

/**
 * Reads the 409 body of createCustomer (ApiError keeps it): the existing
 * profile's id and a ready-to-show message. Null for any other error.
 */
export function customerDuplicateOf(e: unknown): CustomerDuplicate | null {
  if (!(e instanceof ApiError) || e.status !== 409) return null
  const body = (e.body ?? {}) as { existing_id?: unknown }
  if (typeof body.existing_id !== "string" || !body.existing_id) return null
  return { existingId: body.existing_id, message: e.message }
}

export { type PartnerImportResult as CustomerImportResult } from "./partner-import"

/** Bulk-import customers from a CSV file (name required; same name is skipped). */
export function importCustomers(file: File, opts: { tenantId?: string; idempotencyKey?: string } = {}) {
  return uploadPartnerCsv("customers", file, opts)
}

export function customerImportTemplate() {
  return fetchPartnerTemplate("customers")
}
