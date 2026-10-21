import { ApiError } from "@/lib/api/errors"
import { getProduct } from "@/lib/api/products"
import { listUnits } from "@/lib/api/units"

export interface StockShortage {
  productId: string
  available: string
  requested: string
}

/** Quantities are in the product's base unit (backend requested_qty), not the line's converted unit.
 * 422 insufficient_stock carries {details:[{product_id, available_qty, requested_qty}]}; null for any other error. */
export function parseStockShortages(err: unknown): StockShortage[] | null {
  if (!(err instanceof ApiError)) return null
  const b = err.body as { error?: unknown; details?: unknown } | null | undefined
  if (!b || b.error !== "insufficient_stock" || !Array.isArray(b.details)) return null
  const out: StockShortage[] = []
  for (const raw of b.details) {
    const d = raw as Record<string, unknown> | null
    if (!d || typeof d.product_id !== "string") continue
    out.push({
      productId: d.product_id,
      available: String(d.available_qty ?? "?"),
      requested: String(d.requested_qty ?? "?"),
    })
  }
  return out.length > 0 ? out : null
}

/**
 * Plain-Chinese shortage sentence naming each short product, or null when err is not a stock shortage.
 * `names` (product id -> name) is tried first; anything missing is looked up, and falls back to 该商品.
 */
export async function describeStockShortage(
  err: unknown,
  tenantId?: string,
  names: Record<string, string> = {},
): Promise<string | null> {
  const shortages = parseStockShortages(err)
  if (!shortages) return null
  // Quantities are in the base unit, so name it; an unknown unit just leaves the numbers bare.
  let unitNames: Map<string, string> | null = null
  const unitName = async (unitId?: string) => {
    if (!unitId) return ""
    if (!unitNames) {
      try {
        unitNames = new Map((await listUnits(undefined, tenantId)).items.map((u) => [u.id, u.name]))
      } catch {
        unitNames = new Map()
      }
    }
    return unitNames.get(unitId) ?? ""
  }
  const parts = await Promise.all(
    shortages.map(async (s) => {
      let name = names[s.productId]
      let unit = ""
      try {
        const p = await getProduct(s.productId, tenantId)
        name = name || p.name
        unit = await unitName(p.default_unit_id)
      } catch {
        name = name || "该商品"
      }
      return `「${name}」库存不足：可用 ${s.available}${unit}，本单需要 ${s.requested}${unit}`
    }),
  )
  return parts.join("；")
}
