import { apiFetch } from "@/lib/api/client"
import type { MeResponse } from "@/lib/api/me"

/**
 * The signed-in person's shop name from /me, read silently from the browser.
 * "" when unknown; a failure is swallowed because the caller only decorates a page with it.
 */
export async function fetchMyShopName(tenantId?: string, signal?: AbortSignal): Promise<string> {
  try {
    const me = await apiFetch<Partial<MeResponse>>("/me", { tenantId, signal, silent: true, cache: "no-store" })
    return (me.shop_name ?? "").trim()
  } catch {
    return ""
  }
}
