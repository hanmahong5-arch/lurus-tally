/**
 * API wrapper for GET /last-prices — the unit price a customer / supplier was last
 * billed per product (latest approved bill). Products with no history are absent.
 */
import { apiFetch } from "./client"

export type LastPriceKind = "sale" | "purchase"

export interface LastPrice {
  product_id: string
  /** Decimal string, at least two decimals. */
  unit_price: string
  unit_name: string
  /** Unit the line was billed in; absent when the line had none. */
  unit_id?: string
  /** unit_price converted to one base unit. */
  base_unit_price: string
  /** Base units per unit_name. */
  factor: string
  /** "last": from a past bill; "default": the product's default price (no history). */
  source: "last" | "default"
  /** YYYY-MM-DD; empty for source "default". */
  bill_date: string
}

export interface LastPricesResult {
  items: LastPrice[]
}

export async function getLastPrices(
  params: { partnerId?: string; productIds: string[]; type: LastPriceKind },
  opts: { tenantId?: string; signal?: AbortSignal } = {}
): Promise<LastPricesResult> {
  const qs = new URLSearchParams({ product_ids: params.productIds.join(","), type: params.type })
  // No partner (walk-in sale): the server answers with default prices only.
  if (params.partnerId) qs.set("partner_id", params.partnerId)
  return apiFetch<LastPricesResult>(`/last-prices?${qs.toString()}`, {
    tenantId: opts.tenantId,
    signal: opts.signal,
    silent: true,
  })
}

export interface LastPriceHintOpts {
  source?: "last" | "default"
  /** Unit the quoted price is per. */
  unitName?: string
  /** Price per base unit, set when the filled price differs from the quoted one. */
  basePrice?: string
}

/** "上次 ¥9.80/株 · 9/28" or "默认价 ¥12.50" — the grey hint under a pre-filled price. */
export function formatLastPriceHint(price: string, billDate: string, opts: LastPriceHintOpts = {}): string {
  const unit = opts.unitName ? `/${opts.unitName}` : ""
  if (opts.source === "default") return `默认价 ¥${price}${unit}`
  const m = /^\d{4}-(\d{2})-(\d{2})/.exec(billDate)
  const day = m ? ` · ${parseInt(m[1], 10)}/${parseInt(m[2], 10)}` : ""
  const base = opts.basePrice ? `（折合每基本单位 ¥${opts.basePrice}）` : ""
  return `上次 ¥${price}${unit}${day}${base}`
}
