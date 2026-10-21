"use client"

import { useEffect } from "react"
import { markSigningOut, clearCart } from "@/components/pos/cart-storage"
import { SIGNOUT_SIGNAL_KEY } from "@/lib/device-keys"

/**
 * Listens for the sign-out another tab of this browser stamped on the shared device, so a page left open
 * (till, back office, print page) does not keep the last person's screen once the next one signs in.
 * Covers the storage event, the back/forward cache restore and a sign-out that happened before this page loaded.
 */
export function useSignOutBroadcast(): void {
  useEffect(() => {
    // The hidden print frame sits inside a page that listens itself; leaving would only reload the frame.
    if (window.parent !== window) return
    const leave = () => {
      // Stop this tab saving the cart again between the wipe and the page unloading.
      markSigningOut()
      clearCart()
      window.location.assign("/login?switch=1")
    }
    // Derived from the current clock so a skewed timeOrigin (sleep, clock sync) cannot misfire.
    const loadedAt = Date.now() - performance.now()
    // A sign-out stamped after this page loaded means leave now.
    const signedOutAfterLoad = (): boolean => {
      try {
        const signedOutAt = Number(window.localStorage.getItem(SIGNOUT_SIGNAL_KEY))
        return Number.isFinite(signedOutAt) && signedOutAt > loadedAt
      } catch {
        // Storage blocked: nothing to compare.
        return false
      }
    }
    // A tab that missed the broadcast.
    if (signedOutAfterLoad()) {
      leave()
      return
    }
    // The back/forward cache restores the page without mounting it again, so compare again then.
    const onPageShow = (e: PageTransitionEvent) => {
      if (e.persisted && signedOutAfterLoad()) leave()
    }
    const onStorage = (e: StorageEvent) => {
      if (e.key !== SIGNOUT_SIGNAL_KEY || e.newValue === null) return
      // Another tab ended the login: drop this tab's cart before it is saved again, then leave.
      leave()
    }
    window.addEventListener("pageshow", onPageShow)
    window.addEventListener("storage", onStorage)
    return () => {
      window.removeEventListener("storage", onStorage)
      window.removeEventListener("pageshow", onPageShow)
    }
  }, [])
}
