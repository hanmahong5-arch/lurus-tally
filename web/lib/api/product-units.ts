/**
 * API wrapper for GET /products/:id/units — the units a product can be billed in:
 * its base unit plus every unit that has a conversion factor configured.
 */
import { apiFetch } from "./client"

export interface ProductUnitOption {
  id: string
  code: string
  name: string
  /** How many base units one of this unit equals; decimal string. */
  conversion_factor: string
  is_base: boolean
}

export async function listProductUnits(
  productId: string,
  opts: { tenantId?: string; signal?: AbortSignal } = {}
): Promise<ProductUnitOption[]> {
  const res = await apiFetch<{ items: ProductUnitOption[] }>(
    `/products/${encodeURIComponent(productId)}/units`,
    { tenantId: opts.tenantId, signal: opts.signal, silent: true }
  )
  return res.items
}
