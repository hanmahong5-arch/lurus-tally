/**
 * Importing API helpers — multi-platform CSV order import (Amazon / Shopify).
 *
 * POST /api/v1/imports/orders  — upload CSV + platform
 * GET  /api/v1/imports/mappings — list SKU mappings
 *
 * File uploads bypass apiFetch (which sets Content-Type: application/json)
 * so the browser can set the correct multipart/form-data boundary.
 */

export type Platform = "amazon" | "shopify"

export interface ImportedOrder {
  platform_order_no: string
  bill_id?: string
  bill_no?: string
}

export interface SkippedOrder {
  platform_order_no: string
  reason: string
}

export interface OversellRow {
  platform_order_no: string
  product_id: string
  requested_qty: string
  available_qty: string
}

export interface UnknownSKU {
  platform: string
  platform_sku: string
}

export interface ImportSummary {
  total_parsed: number
  imported: number
  skipped: number
  oversell_rows: number
  unknown_skus: number
}

export interface ImportResult {
  imported: ImportedOrder[]
  skipped: SkippedOrder[]
  oversells: OversellRow[]
  unknown_skus: UnknownSKU[]
  summary: ImportSummary
}

export interface SKUMapping {
  id: string
  platform: string
  platform_sku: string
  product_id: string
  updated_at: string
}

export interface ListMappingsResponse {
  items: SKUMapping[]
  total: number
}

export interface SKUHint {
  platform_sku: string
  product_id: string
}

/**
 * Upload a platform CSV file for import.
 *
 * @param file         - The CSV File object from a file input
 * @param platform     - "amazon" | "shopify"
 * @param warehouseId  - UUID of the destination warehouse
 * @param preview      - When true, returns oversell report without creating bills
 * @param hints        - Optional SKU→product_id mapping overrides
 */
export async function uploadOrderCSV(
  file: File,
  platform: Platform,
  warehouseId: string,
  preview = false,
  hints: SKUHint[] = [],
): Promise<ImportResult> {
  const form = new FormData()
  form.append("file", file)
  form.append("platform", platform)
  form.append("warehouse", warehouseId)

  if (hints.length > 0) {
    form.append("hints", JSON.stringify(hints))
  }

  const url = `/api/proxy/imports/orders${preview ? "?preview=true" : ""}`
  const res = await fetch(url, {
    method: "POST",
    body: form,
    cache: "no-store",
  })

  if (!res.ok) {
    let detail = res.statusText
    try {
      const body = (await res.json()) as { detail?: string; error?: string }
      detail = body.detail ?? body.error ?? detail
    } catch {
      // keep statusText
    }
    throw new Error(`import orders: ${res.status} ${detail}`)
  }

  return (await res.json()) as ImportResult
}

/**
 * List all SKU mappings for the current tenant.
 *
 * @param platform - Optional filter: "amazon" | "shopify" | undefined (all)
 */
export async function listMappings(
  platform?: Platform,
  signal?: AbortSignal,
): Promise<ListMappingsResponse> {
  const params = platform ? `?platform=${platform}` : ""
  const res = await fetch(`/api/proxy/imports/mappings${params}`, {
    cache: "no-store",
    signal,
  })
  if (!res.ok) {
    throw new Error(`list mappings: ${res.status} ${res.statusText}`)
  }
  return (await res.json()) as ListMappingsResponse
}
