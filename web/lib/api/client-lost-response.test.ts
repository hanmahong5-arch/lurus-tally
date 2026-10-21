/**
 * A response that never reaches the caller intact (proxy cannot reach the
 * backend, or the 2xx body is cut off) must surface as NetworkError so the
 * submit-key hook keeps the Idempotency-Key for a true retry.
 */
import { describe, it, expect, vi, afterEach } from "vitest"

const toastError = vi.fn()
vi.mock("sonner", () => ({ toast: { error: toastError } }))

import { apiFetch } from "./client"
import { ApiError, NetworkError } from "./errors"

describe("apiFetch lost responses", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    toastError.mockClear()
  })

  it("maps the proxy's 502 upstream_unreachable to NetworkError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ error: "upstream_unreachable" }), {
            status: 502,
            headers: { "content-type": "application/json" },
          })
        )
      )
    )
    const err = await apiFetch("/purchase-bills", { method: "POST", body: "{}" }).catch((e) => e)
    expect(err).toBeInstanceOf(NetworkError)
    expect(toastError).toHaveBeenCalledWith("网络异常，请稍后重试")
  })

  it("keeps an ordinary 502 as ApiError", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("bad gateway", { status: 502 }))))
    const err = await apiFetch("/purchase-bills", { method: "POST", body: "{}" }).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
  })

  it("maps a truncated 2xx body to NetworkError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response('{"id":"x', { status: 200 })))
    )
    const err = await apiFetch("/purchase-bills", { method: "POST", body: "{}" }).catch((e) => e)
    expect(err).toBeInstanceOf(NetworkError)
  })
})
