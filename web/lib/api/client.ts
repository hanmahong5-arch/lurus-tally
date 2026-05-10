/**
 * apiFetch — unified browser-side HTTP client for Tally.
 *
 * Behaviour summary:
 *   - Auto-prefixes /api/proxy unless given an absolute URL
 *   - Auto-sets Content-Type: application/json and X-Tenant-ID
 *   - Network failure → NetworkError (toast unless offline banner is up)
 *   - 401 → trigger re-auth via next-auth signIn, throw UnauthorizedError
 *   - 403 → toast.error("权限不足"), throw ApiError
 *   - 5xx → toast.error("服务暂时不可用"), throw ApiError
 *   - 4xx → throw ApiError (no toast — caller decides UX)
 *   - opts.silent suppresses every toast / signIn side effect
 */

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
    await mod.signIn("zitadel", { callbackUrl })
  } catch {
    // In jsdom / SSR there is no real session — swallow, caller still gets the throw
  }
}

export async function apiFetch<T>(path: string, opts: ApiOptions = {}): Promise<T> {
  const { tenantId, silent, headers, idempotencyKey, ...rest } = opts
  const finalHeaders = mergeHeaders(headers, tenantId)
  const method = (rest.method ?? "GET").toUpperCase()
  if (WRITE_METHODS.has(method) && idempotencyKey !== null && !finalHeaders["Idempotency-Key"]) {
    finalHeaders["Idempotency-Key"] = idempotencyKey ?? newIdempotencyKey()
  }
  const url = buildUrl(path)

  let res: Response
  try {
    res = await fetch(url, { ...rest, headers: finalHeaders })
  } catch (err) {
    const offline = typeof navigator !== "undefined" && navigator.onLine === false
    if (offline) {
      throw new NetworkError("offline", "offline")
    }
    if (!silent) await safeToast("error", "网络异常，请稍后重试")
    throw new NetworkError("network", (err as Error)?.message ?? "network")
  }

  if (res.ok) {
    if (res.status === 204) return undefined as unknown as T
    return (await res.json()) as T
  }

  const { body, message, code } = await readBody(res)

  if (res.status === 401) {
    if (!silent) await triggerSignIn()
    throw new UnauthorizedError(message, body)
  }
  if (res.status === 403) {
    if (!silent) await safeToast("error", "权限不足")
    throw new ApiError(403, code, message, body)
  }
  if (res.status >= 500) {
    if (!silent) await safeToast("error", "服务暂时不可用")
    if (typeof console !== "undefined") console.error("[apiFetch] 5xx", url, message)
    throw new ApiError(res.status, code, message, body)
  }
  // 4xx other than 401/403 — silent at the client layer; callers decide UX.
  throw new ApiError(res.status, code, message, body)
}
