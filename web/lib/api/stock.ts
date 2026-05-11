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
 */
import { apiFetch } from "./client"

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
  signal?: AbortSignal
  retry?: number
}

export interface ListMovementsParams {
  product_id?: string
  warehouse_id?: string
  limit?: number
  offset?: number
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

interface ItemsEnvelope<T> {
  items: T[]
}

function buildQuery(params: Record<string, string | number | undefined>): string {
  const usp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined) usp.set(k, String(v))
  }
  const s = usp.toString()
  return s ? "?" + s : ""
}

/**
 * List stock snapshots across the tenant, optionally filtered by product or warehouse.
 */
export async function listStockSnapshots(
  params: ListSnapshotsParams = {}
): Promise<StockSnapshot[]> {
  const { product_id, warehouse_id, limit, offset, tenantId, signal, retry } = params
  const qs = buildQuery({ product_id, warehouse_id, limit, offset })
  const body = await apiFetch<ItemsEnvelope<StockSnapshot>>("/stock/snapshots" + qs, { tenantId, signal, retry })
  return body.items ?? []
}

/**
 * List stock movement history (append-only ledger), most recent first.
 */
export async function listStockMovements(
  params: ListMovementsParams = {}
): Promise<StockMovement[]> {
  const { product_id, warehouse_id, limit, offset, tenantId, signal, retry } = params
  const qs = buildQuery({ product_id, warehouse_id, limit, offset })
  const body = await apiFetch<ItemsEnvelope<StockMovement>>("/stock/movements" + qs, { tenantId, signal, retry })
  return body.items ?? []
}

/**
 * Fetch all warehouse snapshots for a single product.
 */
export async function getProductStock(
  productId: string,
  tenantId?: string
): Promise<StockSnapshot[]> {
  return listStockSnapshots({ product_id: productId, tenantId, limit: 100 })
}
