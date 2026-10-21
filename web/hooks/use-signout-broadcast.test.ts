import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { renderHook } from "@testing-library/react"

import { useSignOutBroadcast } from "./use-signout-broadcast"
import { resetSigningOut } from "@/components/pos/cart-storage"

const mount = () => renderHook(() => useSignOutBroadcast())

describe("useSignOutBroadcast", () => {
  const orig = window.location
  const assign = vi.fn()
  beforeEach(() => {
    assign.mockReset()
    window.sessionStorage.clear()
    window.localStorage.clear()
    resetSigningOut()
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
  })
  afterEach(() => {
    resetSigningOut()
    Object.defineProperty(window, "location", { configurable: true, value: orig })
  })

  it("goes to the switch login when another tab signs out", () => {
    window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
    mount()
    window.dispatchEvent(new StorageEvent("storage", { key: "other", newValue: "1" }))
    expect(assign).not.toHaveBeenCalled()
    window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: "123" }))
    expect(assign).toHaveBeenCalledWith("/login?switch=1")
    expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
  })

  it("ignores the signal being removed", () => {
    mount()
    window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: null }))
    expect(assign).not.toHaveBeenCalled()
  })

  it("leaves on mount when a sign-out happened after the page loaded", () => {
    window.localStorage.setItem("tally:signed-out-at", String(Date.now() + 1000))
    mount()
    expect(assign).toHaveBeenCalledWith("/login?switch=1")
  })

  it("ignores an older sign-out on mount", () => {
    window.localStorage.setItem("tally:signed-out-at", "1000")
    mount()
    expect(assign).not.toHaveBeenCalled()
  })

  it("leaves when restored from the back/forward cache after a sign-out", () => {
    mount()
    window.localStorage.setItem("tally:signed-out-at", String(Date.now() + 1000))
    window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: false }))
    expect(assign).not.toHaveBeenCalled()
    window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true }))
    expect(assign).toHaveBeenCalledWith("/login?switch=1")
  })

  it("does not leave when running inside a frame", () => {
    const spy = vi.spyOn(window, "parent", "get").mockReturnValue({} as Window)
    try {
      mount()
      window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: "123" }))
      expect(assign).not.toHaveBeenCalled()
    } finally {
      spy.mockRestore()
    }
  })

  it("stops listening after unmount", () => {
    const { unmount } = mount()
    unmount()
    window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: "123" }))
    expect(assign).not.toHaveBeenCalled()
  })
})
