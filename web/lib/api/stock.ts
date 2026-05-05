/**
 * API wrapper for the stock (inventory) endpoints.
 *
 * Backend routes (prefix /api/v1, proxied through /api/proxy):
 *   GET  /stock/snapshots                 — list snapshots, filter by product_id / warehouse_id
 *   GET  /stock/snapshots/:product_id     — list all warehouses for one SKU
 *   GET  /stock/movements                 — list movement history, filter by product_id / warehouse_id
 *
 * Field naming follows the rest of the codebase (snake_case JSON). Decimal-typed
 * fields arrive as strings to preserve precision; the UI is responsible for
 * formatting (Number(x).toFixed) at the leaf.
 *
 * NOTE: At the time of writing the backend domain.Snapshot / domain.Movement
 * structs do not yet carry json tags, so the live response may surface PascalCase
 * keys. Whichever form lands, this wrapper expects the contract documented above
 * (snake_case). Backend agent #9 owns the router wiring and DTO normalisation.
 */
export type Direction = "in" | "out" | "adjust"

export type ReferenceType = "purchase" | "sale" | "adjust" | "transfer" | "init"

export interface StockSnapshot {
  id: string
  tenant_id: string
  product_id: string
  warehouse_id: string
  on_hand_qty: string
  available_qty: string
  unit_cost: string
  cost_strategy?: string
  updated_at: string
}

export interface StockMovement {
  id: string
  tenant_id: string
  product_id: string
  warehouse_id: string
  direction: Direction
  qty_base: string
  unit_cost: string
  total_cost: string
  reference_type: ReferenceType
  reference_id?: string | null
  occurred_at: string
  created_by?: string | null
  note?: string
  created_at: string
}

export interface ListSnapshotsParams {
  product_id?: string
  warehouse_id?: string
  limit?: number
  offset?: number
  tenantId?: string
}

export interface ListMovementsParams {
  product_id?: string
  warehouse_id?: string
  limit?: number
  offset?: number
  tenantId?: string
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
    throw new Error(body.error ?? body.message ?? `${operation}: HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

interface ItemsEnvelope<T> {
  items: T[]
}

/**
 * List stock snapshots across the tenant, optionally filtered by product or warehouse.
 * The backend returns `{ items: [...] }`; this wrapper unwraps to a plain array.
 */
export async function listStockSnapshots(
  params: ListSnapshotsParams = {}
): Promise<StockSnapshot[]> {
  const { product_id, warehouse_id, limit, offset, tenantId } = params
  const url = new URL(BASE + "/stock/snapshots", window.location.origin)
  if (product_id) url.searchParams.set("product_id", product_id)
  if (warehouse_id) url.searchParams.set("warehouse_id", warehouse_id)
  if (limit !== undefined) url.searchParams.set("limit", String(limit))
  if (offset !== undefined) url.searchParams.set("offset", String(offset))

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  const body = await handleResponse<ItemsEnvelope<StockSnapshot>>(res, "listStockSnapshots")
  return body.items ?? []
}

/**
 * List stock movement history (append-only ledger), most recent first.
 */
export async function listStockMovements(
  params: ListMovementsParams = {}
): Promise<StockMovement[]> {
  const { product_id, warehouse_id, limit, offset, tenantId } = params
  const url = new URL(BASE + "/stock/movements", window.location.origin)
  if (product_id) url.searchParams.set("product_id", product_id)
  if (warehouse_id) url.searchParams.set("warehouse_id", warehouse_id)
  if (limit !== undefined) url.searchParams.set("limit", String(limit))
  if (offset !== undefined) url.searchParams.set("offset", String(offset))

  const res = await fetch(url.toString(), { headers: headers(tenantId) })
  const body = await handleResponse<ItemsEnvelope<StockMovement>>(res, "listStockMovements")
  return body.items ?? []
}

/**
 * Fetch all warehouse snapshots for a single product. Implemented client-side as
 * a filtered list call so the page works regardless of whether the backend
 * exposes a dedicated `/stock/snapshots/:product_id` route.
 */
export async function getProductStock(
  productId: string,
  tenantId?: string
): Promise<StockSnapshot[]> {
  return listStockSnapshots({ product_id: productId, tenantId, limit: 100 })
}
