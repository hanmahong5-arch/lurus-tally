import Decimal from "decimal.js"
import type { CartItem, CartState, DiscountType } from "@/lib/pos/cart-reducer"
import type { MeasurementStrategy } from "@/lib/api/products"
import { AI_HISTORY_KEY, COPY_BILL_PREFIX, SIGNOUT_SIGNAL_KEY } from "@/lib/device-keys"
import { forgetAllPendingRefunds } from "@/lib/pos/pending-refund"

const KEY = "tally.pos.cart.v1"

interface StoredItem {
  productId: string
  productName: string
  unitId: string
  unitName: string
  unitPrice: string
  quantity: string
  measurementStrategy: MeasurementStrategy
}

interface StoredCart {
  items: StoredItem[]
  discount: string
  discountType: DiscountType
  remark: string
  savedAt?: number
}

// In-memory only: set once this page has started signing out, so a scan or key press that lands
// while the sign-out is in flight cannot write the cart back after it was wiped.
let signingOut = false

/** markSigningOut makes saveCart a no-op for the rest of this page's life. */
export function markSigningOut(): void {
  signingOut = true
}

/** resetSigningOut lifts the mark again (tests, or a sign-out that did not go through). */
export function resetSigningOut(): void {
  signingOut = false
}

/**
 * Keep the open cart in sessionStorage so a sign-in redirect (expired session) or a
 * page refresh does not lose a half-rung sale. Decimals are stored as strings.
 */
export function saveCart(state: CartState): void {
  if (typeof window === "undefined" || signingOut) return
  try {
    if (state.items.length === 0) {
      window.sessionStorage.removeItem(KEY)
      return
    }
    const stored: StoredCart = {
      items: state.items.map((i) => ({
        productId: i.productId,
        productName: i.productName,
        unitId: i.unitId,
        unitName: i.unitName,
        unitPrice: i.unitPrice.toString(),
        quantity: i.quantity.toString(),
        measurementStrategy: i.measurementStrategy,
      })),
      discount: state.discount.toString(),
      discountType: state.discountType,
      remark: state.remark,
      savedAt: Date.now(),
    }
    window.sessionStorage.setItem(KEY, JSON.stringify(stored))
  } catch {
    // Storage full or blocked: the cart simply is not remembered.
  }
}

function savedBeforeLastSignOut(savedAt: number | undefined): boolean {
  const raw = window.localStorage.getItem(SIGNOUT_SIGNAL_KEY)
  if (!raw) return false
  const signedOutAt = Number(raw)
  if (!Number.isFinite(signedOutAt)) return false
  return typeof savedAt !== "number" || savedAt <= signedOutAt
}

/** loadCart returns the saved cart, or null when nothing (valid) was saved. */
export function loadCart(): CartState | null {
  if (typeof window === "undefined") return null
  try {
    const raw = window.sessionStorage.getItem(KEY)
    if (!raw) return null
    const s = JSON.parse(raw) as StoredCart
    if (!Array.isArray(s.items) || s.items.length === 0) return null
    if (savedBeforeLastSignOut(s.savedAt)) {
      // A tab that missed the sign-out signal must not hand the last person's cart to the next one.
      window.sessionStorage.removeItem(KEY)
      return null
    }
    const items: CartItem[] = s.items.map((i) => ({
      productId: String(i.productId),
      productName: String(i.productName),
      unitId: String(i.unitId ?? ""),
      unitName: String(i.unitName ?? ""),
      unitPrice: new Decimal(i.unitPrice),
      quantity: new Decimal(i.quantity),
      measurementStrategy: i.measurementStrategy,
    }))
    return {
      items,
      discount: new Decimal(s.discount ?? "0"),
      discountType: s.discountType === "percent" ? "percent" : "fixed",
      remark: typeof s.remark === "string" ? s.remark : "",
    }
  } catch {
    return null
  }
}

export function clearCart(): void {
  if (typeof window === "undefined") return
  try {
    window.sessionStorage.removeItem(KEY)
  } catch {
    // ignore
  }
}

/** clearDeviceTraces drops the open cart, unbooked refunds, copied-bill drafts and chat history on this device. */
export function clearDeviceTraces(): void {
  clearCart()
  forgetAllPendingRefunds()
  if (typeof window === "undefined") return
  try {
    const ss = window.sessionStorage
    const copies: string[] = []
    for (let i = 0; i < ss.length; i++) {
      const k = ss.key(i)
      if (k && k.startsWith(COPY_BILL_PREFIX)) copies.push(k)
    }
    copies.forEach((k) => ss.removeItem(k))
    window.localStorage.removeItem(AI_HISTORY_KEY)
  } catch {
    // ignore
  }
}
