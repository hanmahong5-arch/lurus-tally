/**
 * apiFetch must treat a caller-initiated abort (effect cleanup, navigation) as
 * silent cancellation — never as a network failure toast.
 */
import { describe, it, expect, vi, afterEach } from "vitest"

const toastError = vi.fn()
vi.mock("sonner", () => ({ toast: { error: toastError } }))

import { apiFetch } from "./client"
import { NetworkError } from "./errors"

describe("apiFetch abort handling", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    toastError.mockClear()
  })

  it("rethrows the AbortError without a network toast when the signal is aborted", async () => {
    const ctrl = new AbortController()
    vi.stubGlobal(
      "fetch",
      vi.fn(() => {
        ctrl.abort()
        return Promise.reject(new DOMException("The operation was aborted.", "AbortError"))
      })
    )
    const err = await apiFetch("/purchase-bills", { signal: ctrl.signal }).catch((e) => e)
    expect(err).toBeInstanceOf(DOMException)
    expect((err as DOMException).name).toBe("AbortError")
    expect(err).not.toBeInstanceOf(NetworkError)
    expect(toastError).not.toHaveBeenCalled()
  })

  it("still toasts a genuine network failure", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.reject(new TypeError("Failed to fetch"))))
    const err = await apiFetch("/purchase-bills").catch((e) => e)
    expect(err).toBeInstanceOf(NetworkError)
    expect(toastError).toHaveBeenCalledWith("网络异常，请稍后重试")
  })
})
