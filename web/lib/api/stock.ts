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
  tenantId?: string,
  signal?: AbortSignal
): Promise<StockSnapshot[]> {
  return listStockSnapshots({ product_id: productId, tenantId, limit: 100, signal })
}

// ---------------------------------------------------------------------------
// Server-side helpers (RSC / Server Actions)
// Call the backend in-cluster directly with an access token.  Never import
// these from client components — they rely on server-only env vars.
// ---------------------------------------------------------------------------

const SERVER_BACKEND_URL =
  typeof window === "undefined"
    ? (process.env.BACKEND_URL ?? "http://tally-backend:18200")
    : ""

// One product at or below its auto-computed reorder point. Per-product
// granularity (a reorder decision is per-product); the backend sums available
// across warehouses. `reorder_point` is the learned ROP, or an explicit
// low_safe_qty when set. `days_of_supply` ≈ available / avg_daily_sales.
export interface LowStockItem {
  product_id: string
  product_code: string
  product_name: string
  available_qty: string
  reorder_point: string
  avg_daily_sales: string
  days_of_supply: string
}

export interface LowStockResponse {
  items: LowStockItem[]
  count: number
}

/**
 * Server-side: GET /api/v1/stock/alerts/low-stock
 * Requires the user's access_token from auth().
 */
export async function fetchLowStockAlerts(
  accessToken: string,
  limit = 5,
): Promise<LowStockResponse> {
  const res = await fetch(
    `${SERVER_BACKEND_URL}/api/v1/stock/alerts/low-stock?limit=${limit}`,
    {
      headers: { Authorization: `Bearer ${accessToken}` },
      next: { revalidate: 60 },
    },
  )
  if (!res.ok) return { items: [], count: 0 }
  return (await res.json()) as LowStockResponse
}

/**
 * Server-side: GET /api/v1/purchase-bills?status=0&size=1
 * Returns the `total` count of draft purchase bills.
 */
export async function fetchDraftPurchaseBillCount(accessToken: string): Promise<number> {
  const res = await fetch(
    `${SERVER_BACKEND_URL}/api/v1/purchase-bills?status=0&size=1`,
    {
      headers: { Authorization: `Bearer ${accessToken}` },
      next: { revalidate: 60 },
    },
  )
  if (!res.ok) return 0
  const body = (await res.json()) as { total?: number }
  return body.total ?? 0
}
