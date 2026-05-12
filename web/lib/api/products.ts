/**
 * API wrapper for the product catalogue endpoints.
 * All functions accept an optional tenantId that is forwarded as X-Tenant-ID header
 * for development; Story 2.1 will replace this with session-cookie auth.
 */
import { apiFetch } from "./client"

export type MeasurementStrategy =
  | "individual"
  | "weight"
  | "length"
  | "volume"
  | "batch"
  | "serial"

export interface Product {
  id: string
  tenant_id: string
  category_id?: string
  code: string
  name: string
  manufacturer?: string
  model?: string
  spec?: string
  brand?: string
  mnemonic?: string
  color?: string
  expiry_days?: number
  weight_kg?: string
  enabled: boolean
  enable_serial_no: boolean
  enable_lot_no: boolean
  shelf_position?: string
  img_urls?: string[]
  remark?: string
  measurement_strategy: MeasurementStrategy
  default_unit_id?: string
  attributes: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface CreateProductInput {
  code: string
  name: string
  category_id?: string
  manufacturer?: string
  model?: string
  spec?: string
  brand?: string
  mnemonic?: string
  color?: string
  expiry_days?: number
  weight_kg?: string
  enable_serial_no?: boolean
  enable_lot_no?: boolean
  shelf_position?: string
  img_urls?: string[]
  remark?: string
  measurement_strategy?: MeasurementStrategy
  default_unit_id?: string
  attributes?: Record<string, unknown>
}

export interface UpdateProductInput extends Partial<CreateProductInput> {
  enabled?: boolean
}

export interface ListProductsResponse {
  items: Product[]
  total: number
}

export interface ListProductsParams {
  q?: string
  limit?: number
  offset?: number
  enabled?: boolean
  attributes_filter?: Record<string, unknown>
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export async function listProducts(
  params: ListProductsParams = {}
): Promise<ListProductsResponse> {
  const { q, limit = 20, offset = 0, enabled, attributes_filter, tenantId, signal, retry } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  if (enabled !== undefined) usp.set("enabled", String(enabled))
  if (attributes_filter) usp.set("attributes_filter", JSON.stringify(attributes_filter))
  return apiFetch<ListProductsResponse>(`/products?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getProduct(id: string, tenantId?: string, signal?: AbortSignal): Promise<Product> {
  return apiFetch<Product>(`/products/${id}`, { tenantId, signal })
}

export async function createProduct(
  input: CreateProductInput,
  tenantId?: string
): Promise<Product> {
  return apiFetch<Product>("/products", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function updateProduct(
  id: string,
  input: UpdateProductInput,
  tenantId?: string
): Promise<Product> {
  return apiFetch<Product>(`/products/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteProduct(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/products/${id}`, { method: "DELETE", tenantId })
}

export async function restoreProduct(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/products/${id}/restore`, { method: "POST", tenantId })
}
