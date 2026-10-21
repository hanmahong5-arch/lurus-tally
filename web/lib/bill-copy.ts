/**
 * "复制为新单" hand-off. The detail page parks the bill's content in sessionStorage and
 * the new-bill page takes it once on mount — nothing is saved on the server.
 */

import { COPY_BILL_PREFIX } from "@/lib/device-keys"

export type BillCopyKind = "sale" | "purchase"

export interface BillCopyItem {
  product_id: string
  product_name?: string
  unit_id?: string
  unit_name?: string
  qty: string
  unit_price: string
}

export interface BillCopyPayload {
  partner_id?: string
  partner_name?: string
  warehouse_id?: string
  remark?: string
  shipping_fee?: string
  tax_amount?: string
  /** Foreign-currency bills carry their currency and rate; unit prices are in that currency. */
  currency?: string
  exchange_rate?: string
  items: BillCopyItem[]
}

const keyOf = (kind: BillCopyKind) => `${COPY_BILL_PREFIX}${kind}`

export function saveBillCopy(kind: BillCopyKind, payload: BillCopyPayload): void {
  try {
    sessionStorage.setItem(keyOf(kind), JSON.stringify(payload))
  } catch {
    // Storage blocked: the new page simply opens empty.
  }
}

/** Reads and clears the parked copy; null when there is none or it is unreadable. */
export function takeBillCopy(kind: BillCopyKind): BillCopyPayload | null {
  try {
    const raw = sessionStorage.getItem(keyOf(kind))
    if (!raw) return null
    sessionStorage.removeItem(keyOf(kind))
    const p = JSON.parse(raw) as BillCopyPayload
    return Array.isArray(p?.items) ? p : null
  } catch {
    return null
  }
}
