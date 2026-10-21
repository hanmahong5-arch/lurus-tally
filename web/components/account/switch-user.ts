import { signOut } from "next-auth/react"
import { toast } from "sonner"

import { clearDeviceTraces, markSigningOut, resetSigningOut } from "@/components/pos/cart-storage"
import type { ConfirmDialogOptions } from "@/components/ui/confirm-dialog"
import { SIGNOUT_FAILED_EVENT, SIGNOUT_SIGNAL_KEY, SWITCH_COOKIE } from "@/lib/device-keys"
import { countPendingRefunds } from "@/lib/pos/pending-refund"
import type { ShopRole } from "@/lib/roles"

// Long enough to cover a weekend or holiday between one cashier closing and the next one opening the till.
const SWITCH_COOKIE_MAX_AGE = 30 * 24 * 60 * 60

export type ConfirmFn = (options: ConfirmDialogOptions) => Promise<boolean>

/** clearSwitchMarker is called once someone is signed in again, so the marker does not outlive the hand-over. */
export function clearSwitchMarker(): void {
  if (typeof document === "undefined") return
  document.cookie = `${SWITCH_COOKIE}=; path=/; max-age=0; samesite=lax`
}

/**
 * Ends the login on a device that several people share: wipes what the last person left
 * behind (open cart, unbooked refunds) and sends the next one to a sign-in that always asks
 * for the account again instead of reusing the identity provider's session.
 * returnTo (a site path, e.g. an invite link) is where the next sign-in lands.
 */
export async function signOutForSwitch(
  confirm: ConfirmFn,
  role?: ShopRole | null,
  returnTo?: string,
): Promise<unknown> {
  const pending = countPendingRefunds()
  if (pending > 0) {
    const ok = await confirm({
      title: "还有退款没记上",
      body:
        role === "owner" || role === "manager"
          ? `还有 ${pending} 笔退款没记上，退出后将不再提醒，请先补记。`
          : role === "cashier"
            ? `还有 ${pending} 笔退款没记上，退出后将不再提醒，请先告诉老板或店长。`
            : `还有 ${pending} 笔退款没记上，退出后将不再提醒，请先补记，或告诉老板、店长。`,
      confirmText: "仍要退出",
      cancelText: "先不退出",
      danger: true,
    })
    if (!ok) return undefined
  }
  const back = returnTo && returnTo.startsWith("/") && !returnTo.startsWith("//") ? returnTo : ""
  const target = back ? `/login?switch=1&callbackUrl=${encodeURIComponent(back)}` : "/login?switch=1"
  // Stops the till saving carts while the request is in flight; nothing is wiped until it succeeds.
  markSigningOut()
  let result: unknown
  try {
    result = await signOut({ redirect: false, redirectTo: target })
  } catch {
    // Offline or the sign-out call failed: still signed in, nothing was wiped, so the till must save carts again.
    resetSigningOut()
    // Items scanned while the request was running were not saved; the till page saves its cart now.
    window.dispatchEvent(new Event(SIGNOUT_FAILED_EVENT))
    toast.error("退出没有成功，请检查网络后再试。")
    return undefined
  }
  clearDeviceTraces()
  // 离线壳缓存的页面内嵌上一个登录人的会话，退出时一并清掉。
  try {
    if (typeof caches !== "undefined") void caches.delete("pages").catch(() => {})
  } catch {
    // 缓存接口不可用：忽略。
  }
  try {
    // Other tabs of this browser hear this through the storage event and drop their own cart.
    window.localStorage.setItem(SIGNOUT_SIGNAL_KEY, String(Date.now()))
  } catch {
    // Storage blocked: other tabs simply are not told.
  }
  document.cookie = `${SWITCH_COOKIE}=1; path=/; max-age=${SWITCH_COOKIE_MAX_AGE}; samesite=lax`
  window.location.assign(target)
  return result
}
