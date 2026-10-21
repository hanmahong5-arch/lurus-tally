/**
 * Cash handed over at the till is not booked on the bill (only the part that settles it is),
 * so the checkout screen leaves it here for the receipt to print 实收 / 找零 — on the first
 * print and on any later reprint from this device.
 */
const KEY_PREFIX = "tally-pos-tender:"
/** One key per sale would fill the browser quota over a year; only the latest records are kept. */
const KEEP_LATEST = 500
const KEEP_MAX_AGE_MS = 30 * 24 * 3600 * 1000

export interface Tender {
  received: string
  change: string
  /** Epoch ms when saved; absent on records written before pruning existed. */
  at?: number
}

function prune(): void {
  const store = window.localStorage
  const now = Date.now()
  const rows: { key: string; at: number }[] = []
  for (let i = 0; i < store.length; i++) {
    const key = store.key(i)
    if (!key || !key.startsWith(KEY_PREFIX)) continue
    let at = 0
    try {
      at = (JSON.parse(store.getItem(key) ?? "null") as Partial<Tender> | null)?.at ?? 0
    } catch {
      // unreadable record: treated as oldest
    }
    rows.push({ key, at })
  }
  rows.sort((a, b) => b.at - a.at)
  rows.forEach((r, i) => {
    if (i >= KEEP_LATEST || now - r.at > KEEP_MAX_AGE_MS) store.removeItem(r.key)
  })
}

export function saveTender(billId: string, tender: Tender): void {
  try {
    prune()
    window.localStorage.setItem(KEY_PREFIX + billId, JSON.stringify({ ...tender, at: Date.now() }))
  } catch {
    // storage unavailable: the receipt falls back to the booked amount
  }
}

export function loadTender(billId: string): Tender | null {
  try {
    const raw = window.localStorage.getItem(KEY_PREFIX + billId)
    if (!raw) return null
    const v = JSON.parse(raw) as Partial<Tender>
    return typeof v.received === "string" && typeof v.change === "string" ? { received: v.received, change: v.change } : null
  } catch {
    return null
  }
}

const TENDER_REMARK = /实收\s*(-?\d+(?:\.\d+)?)\s*[，,]\s*找零\s*(-?\d+(?:\.\d+)?)/

/**
 * The cash tendered and change are written on the checkout payment's remark, so any device can
 * reprint them; the localStorage record above is only the fallback for older bills.
 */
export function tenderFromPayments(payments?: { remark?: string }[]): Tender | null {
  for (const p of payments ?? []) {
    const m = p.remark ? TENDER_REMARK.exec(p.remark) : null
    if (m) return { received: m[1], change: m[2] }
  }
  return null
}
