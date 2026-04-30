/**
 * API wrapper for the product catalogue endpoints.
 * All functions accept an optional tenantId that is forwarded as X-Tenant-ID header
 * for development; Story 2.1 will replace this with session-cookie auth.
 */

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
}

const BASE = "/api/proxy"

function headers(tenantId?: string): HeadersInit {
  const h: HeadersInit = { "Content-Type": "application/json" }
  if (tenantId) {
    ;(h as Record<string, string>)["X-Tenant-ID"] = tenantId
  }
  return h
}

export async function listProducts(
  params: ListProductsParams = {}
): Promise<ListProductsResponse> {
  const { q, limit = 20, offset = 0, enabled, attributes_filter, tenantId } =
    params
  const url = new URL(BASE + "/products", window.location.origin)
  if (q) url.searchParams.set("q", q)
  url.searchParams.set("limit", String(limit))
  url.searchParams.set("offset", String(offset))
  if (enabled !== undefined) url.searchParams.set("enabled", String(enabled))
  if (attributes_filter)
    url.searchParams.set("attributes_filter", JSON.stringify(attributes_filter))

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `listProducts: HTTP ${res.status}`)
  }
  return res.json()
}

export async function getProduct(
  id: string,
  tenantId?: string
): Promise<Product> {
  const res = await fetch(`${BASE}/products/${id}`, {
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `getProduct: HTTP ${res.status}`)
  }
  return res.json()
}

export async function createProduct(
  input: CreateProductInput,
  tenantId?: string
): Promise<Product> {
  const res = await fetch(`${BASE}/products`, {
    method: "POST",
    headers: headers(tenantId),
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `createProduct: HTTP ${res.status}`)
  }
  return res.json()
}

export async function updateProduct(
  id: string,
  input: UpdateProductInput,
  tenantId?: string
): Promise<Product> {
  const res = await fetch(`${BASE}/products/${id}`, {
    method: "PUT",
    headers: headers(tenantId),
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `updateProduct: HTTP ${res.status}`)
  }
  return res.json()
}

export async function deleteProduct(
  id: string,
  tenantId?: string
): Promise<void> {
  const res = await fetch(`${BASE}/products/${id}`, {
    method: "DELETE",
    headers: headers(tenantId),
  })
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `deleteProduct: HTTP ${res.status}`)
  }
}

export async function restoreProduct(
  id: string,
  tenantId?: string
): Promise<void> {
  const res = await fetch(`${BASE}/products/${id}/restore`, {
    method: "POST",
    headers: headers(tenantId),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `restoreProduct: HTTP ${res.status}`)
  }
}
