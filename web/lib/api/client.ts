/**
 * apiFetch — unified browser-side HTTP client for Tally.
 *
 * Behaviour summary:
 *   - Auto-prefixes /api/proxy unless given an absolute URL
 *   - Auto-sets Content-Type: application/json and X-Tenant-ID
 *   - Writes (POST/PUT/PATCH/DELETE) get a fresh Idempotency-Key
 *   - Writes preflight `navigator.onLine === false` → NetworkError("offline") without dispatch
 *   - GETs may opt into transient retry (5xx / NetworkError) via `retry`
 *   - Network failure → NetworkError (toast unless offline banner is up)
 *   - 401 → trigger re-auth via next-auth signIn, throw UnauthorizedError
 *   - 403 → toast.error(backend Chinese message, else "权限不足"), throw ApiError
 *   - 402 subscription_expired → one toast per page load (owner: 去订阅页 action), throw ApiError
 *   - 5xx → toast.error("服务暂时不可用"), throw ApiError
 *   - 4xx → throw ApiError (no toast — caller decides UX)
 *   - opts.silent suppresses every toast / signIn side effect
 */

import { normalizeRole } from "@/lib/roles"
import { reportWebError } from "@/lib/telemetry"
import { ApiError, NetworkError, UnauthorizedError } from "./errors"

export interface ApiOptions extends RequestInit {
  tenantId?: string
  /** Skip the global toast and 401 redirect (caller wants to show their own UX). */
  silent?: boolean
  /**
   * Override the auto-generated Idempotency-Key. Set to `null` to disable for
   * streaming or non-idempotent operations the backend explicitly does not
   * dedupe. Default: a fresh UUID v4 is generated per write request.
   */
  idempotencyKey?: string | null
  /**
   * Max number of automatic retries on transient failures (NetworkError / 5xx).
   * Only honoured for GET — writes are never retried at this layer because the
   * server may have applied them before the connection broke. Default: 0.
   * Backoff is 200ms, 400ms, 800ms... (capped by the number of retries).
   */
  retry?: number
}

const PROXY_PREFIX = "/api/proxy"

const WRITE_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"])

function newIdempotencyKey(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID()
  }
  // Fallback for environments without crypto.randomUUID (very old jsdom).
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 12)
}

function buildUrl(path: string): string {
  if (/^https?:\/\//i.test(path)) return path
  if (path.startsWith(PROXY_PREFIX)) return path
  return PROXY_PREFIX + (path.startsWith("/") ? path : "/" + path)
}

function mergeHeaders(init: HeadersInit | undefined, tenantId?: string): Record<string, string> {
  const out: Record<string, string> = { "Content-Type": "application/json" }
  if (init) {
    if (init instanceof Headers) {
      init.forEach((v, k) => { out[k] = v })
    } else if (Array.isArray(init)) {
      for (const [k, v] of init) out[k] = v
    } else {
      Object.assign(out, init as Record<string, string>)
    }
  }
  if (tenantId && !out["X-Tenant-ID"]) out["X-Tenant-ID"] = tenantId
  return out
}

async function readBody(res: Response): Promise<{ body: Record<string, unknown>; message: string; code: string }> {
  let body: Record<string, unknown> = {}
  try {
    body = (await res.json()) as Record<string, unknown>
  } catch {
    // Non-JSON body — fall through with defaults
  }
  const detail = typeof body.detail === "string" ? body.detail : undefined
  const message = typeof body.message === "string" ? body.message : undefined
  const errorField = typeof body.error === "string" ? body.error : undefined
  const finalMessage = detail ?? message ?? errorField ?? `HTTP ${res.status}`
  const code = errorField ?? "http_error"
  return { body, message: finalMessage, code }
}

/** Lazy toast import — avoid SSR issues and keeps tests free of UI noise. */
async function safeToast(level: "error", text: string): Promise<void> {
  try {
    const mod = await import("sonner")
    mod.toast[level](text)
  } catch {
    // sonner not yet installed or not in DOM — degrade silently
  }
}

async function triggerSignIn(): Promise<void> {
  try {
    const mod = await import("next-auth/react")
    const callbackUrl =
      typeof window !== "undefined" ? window.location.pathname + window.location.search : "/"
    await mod.signIn("oidc", { callbackUrl })
  } catch {
    // In jsdom / SSR there is no real session — swallow, caller still gets the throw
  }
}

let subscriptionToastShown = false

/** The shop is read-only (plan lapsed): tell the person once, with the way out for the owner. */
async function toastSubscriptionExpired(): Promise<void> {
  if (subscriptionToastShown) return
  subscriptionToastShown = true
  let isOwner = false
  try {
    const { getSession } = await import("next-auth/react")
    const session = await getSession()
    const user = session?.user as { role?: string | null; isOwner?: boolean } | undefined
    isOwner = normalizeRole(user?.role, user?.isOwner === true) === "owner"
  } catch {
    // no session in this environment — treat as a non-owner
  }
  try {
    const { toast } = await import("sonner")
    if (isOwner) {
      toast.error("套餐已到期，现在只能查看和导出", {
        id: "subscription-expired",
        action: {
          label: "去订阅页",
          onClick: () => {
            if (typeof window !== "undefined") window.location.assign("/subscription")
          },
        },
      })
    } else {
      toast.error("套餐已到期，现在只能查看和导出，请联系老板续费", { id: "subscription-expired" })
    }
  } catch {
    // sonner unavailable — degrade silently
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export async function apiFetch<T>(path: string, opts: ApiOptions = {}): Promise<T> {
  const { tenantId, silent, headers, idempotencyKey, retry, ...rest } = opts
  const finalHeaders = mergeHeaders(headers, tenantId)
  const method = (rest.method ?? "GET").toUpperCase()
  const isWrite = WRITE_METHODS.has(method)
  if (isWrite && idempotencyKey !== null && !finalHeaders["Idempotency-Key"]) {
    finalHeaders["Idempotency-Key"] = idempotencyKey ?? newIdempotencyKey()
  }
  // Offline preflight — don't fire writes into the void. Reads are still
  // attempted (the browser may have stale `onLine === false` while a cached
  // SW or VPN brings the link back).
  if (isWrite && typeof navigator !== "undefined" && navigator.onLine === false) {
    if (!silent) await safeToast("error", "网络已断开，请检查后重试")
    throw new NetworkError("offline", "offline", false)
  }
  const url = buildUrl(path)

  const maxRetries = method === "GET" && typeof retry === "number" && retry > 0 ? retry : 0
  let attempt = 0
  while (true) {
    let res: Response
    try {
      res = await fetch(url, { ...rest, headers: finalHeaders })
    } catch (err) {
      // Caller-initiated cancellation (effect cleanup, route change) is not a
      // network failure: rethrow as-is so callers' `signal.aborted` checks see
      // it, and never toast.
      if (rest.signal?.aborted || (err as Error)?.name === "AbortError") throw err
      const offline = typeof navigator !== "undefined" && navigator.onLine === false
      if (offline) {
        throw new NetworkError("offline", "offline", false)
      }
      if (attempt < maxRetries && (rest.signal == null || !rest.signal.aborted)) {
        await sleep(200 * 2 ** attempt)
        attempt++
        continue
      }
      if (!silent) await safeToast("error", "网络异常，请稍后重试")
      // fetch rejected: the connection failed before any reply, treated as never sent.
      throw new NetworkError("network", (err as Error)?.message ?? "network", false)
    }

    if (res.ok) {
      if (res.status === 204) return undefined as unknown as T
      try {
        return (await res.json()) as T
      } catch (err) {
        if (rest.signal?.aborted || (err as Error)?.name === "AbortError") throw err
        // 2xx whose body was cut off: the caller never saw the result, so a
        // retry with the same Idempotency-Key is the right move.
        if (!silent) await safeToast("error", "网络异常，请稍后重试")
        throw new NetworkError("network", (err as Error)?.message ?? "truncated response")
      }
    }

    const { body, message, code } = await readBody(res)

    // The proxy could not reach the backend (the write may or may not have
    // landed): same as a lost response.
    if (res.status === 502 && code === "upstream_unreachable") {
      if (attempt < maxRetries) {
        await sleep(200 * 2 ** attempt)
        attempt++
        continue
      }
      if (!silent) await safeToast("error", "网络异常，请稍后重试")
      throw new NetworkError("network", code)
    }

    if (res.status === 401) {
      if (!silent) await triggerSignIn()
      throw new UnauthorizedError(message, body)
    }
    if (res.status === 403) {
      if (!silent) await safeToast("error", /[一-鿿]/.test(message) ? message : "权限不足")
      throw new ApiError(403, code, message, body)
    }
    if (res.status === 402 && body.code === "subscription_expired") {
      if (!silent) await toastSubscriptionExpired()
      throw new ApiError(402, "subscription_expired", message, body)
    }
    if (res.status >= 500) {
      if (attempt < maxRetries) {
        await sleep(200 * 2 ** attempt)
        attempt++
        continue
      }
      if (!silent) await safeToast("error", "服务暂时不可用")
      if (typeof console !== "undefined") console.error("[apiFetch] 5xx", url, message)
      reportWebError("api", `${method} ${url.split("?")[0]} ${res.status}`, {
        path: url.split("?")[0],
        request_id: res.headers?.get?.("x-request-id") ?? undefined,
      })
      throw new ApiError(res.status, code, message, body)
    }
    // 4xx other than 401/403 — silent at the client layer; callers decide UX.
    throw new ApiError(res.status, code, message, body)
  }
}
