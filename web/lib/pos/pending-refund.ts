import type { PendingRefund } from "@/components/sale-return-dialog"
import { PENDING_REFUND_PREFIX as KEY_PREFIX } from "@/lib/device-keys"

// A refund that failed to book is remembered on this device so the sale's detail page can offer 补记退款 later.
export function rememberPendingRefund(p: PendingRefund): void {
  try {
    window.localStorage.setItem(KEY_PREFIX + p.saleId, JSON.stringify(p))
  } catch {
    // Storage may be unavailable (private mode); the in-page prompt still works.
  }
}

export function forgetPendingRefund(saleId: string): void {
  try {
    window.localStorage.removeItem(KEY_PREFIX + saleId)
  } catch {
    // Nothing to clean up.
  }
}

export function readPendingRefund(saleId: string): PendingRefund | null {
  try {
    const raw = window.localStorage.getItem(KEY_PREFIX + saleId)
    if (!raw) return null
    const p = JSON.parse(raw) as Partial<PendingRefund>
    if (p.saleId === saleId && p.returnBillId && p.amount && p.method) return p as PendingRefund
  } catch {
    // Corrupt entry: treat as none.
  }
  return null
}

/** countPendingRefunds is how many unbooked refunds this device still remembers. */
export function countPendingRefunds(): number {
  return pendingRefundKeys().length
}

/** forgetAllPendingRefunds drops every remembered unbooked refund (the till changes hands). */
export function forgetAllPendingRefunds(): void {
  try {
    pendingRefundKeys().forEach((k) => window.localStorage.removeItem(k))
  } catch {
    // Nothing to clean up.
  }
}

function pendingRefundKeys(): string[] {
  const keys: string[] = []
  try {
    const ls = window.localStorage
    for (let i = 0; i < ls.length; i++) {
      const k = ls.key(i)
      if (k && k.startsWith(KEY_PREFIX)) keys.push(k)
    }
  } catch {
    // Storage unavailable: nothing remembered.
  }
  return keys
}
