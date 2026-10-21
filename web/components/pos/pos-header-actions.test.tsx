import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

const signOut = vi.fn()
vi.mock("next-auth/react", () => ({
  signOut: (...a: unknown[]) => signOut(...a),
  useSession: () => ({ data: { user: { id: "u1", tenantId: "t1" } }, status: "authenticated" }),
}))
const countUnsent = vi.fn()
vi.mock("@/lib/pos/checkout-queue", () => ({
  countUnsent: (...a: unknown[]) => countUnsent(...a),
  queueKey: (t: string, u: string) => `q@${t}:${u}`,
}))
vi.mock("@/hooks/use-role", () => ({ useRole: () => ({ role: "cashier" }) }))

const confirmFn = vi.fn()
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => confirmFn }))

import { SignOutButton } from "./pos-header-actions"
import { saveCart, resetSigningOut } from "./cart-storage"
import Decimal from "decimal.js"

const cartState = {
  items: [{ productId: "p", productName: "n", unitId: "u", unitName: "个", unitPrice: new Decimal(1), quantity: new Decimal(1), measurementStrategy: "individual" as const }],
  discount: new Decimal(0),
  discountType: "fixed" as const,
  remark: "",
}

describe("SignOutButton", () => {
  beforeEach(() => {
    signOut.mockReset()
    window.sessionStorage.clear()
    window.localStorage.clear()
    confirmFn.mockReset()
    countUnsent.mockReset()
    countUnsent.mockResolvedValue(0)
    resetSigningOut()
  })

  it("asks before leaving when offline checkouts are not uploaded yet, and keeps them if declined", async () => {
    countUnsent.mockResolvedValue(2)
    confirmFn.mockResolvedValue(false)
    render(<SignOutButton />)
    fireEvent.click(screen.getByText("退出登录"))
    await waitFor(() => expect(confirmFn).toHaveBeenCalled())
    expect(confirmFn).toHaveBeenCalledWith(
      expect.objectContaining({ body: "有 2 笔离线结账尚未上传，退出后将在下次登录继续上传，确定退出？" }),
    )
    expect(signOut).not.toHaveBeenCalled()
  })

  it("wipes the cart and unbooked refunds once confirmed, then signs out to the switch-user login", async () => {
    confirmFn.mockResolvedValue(true)
    window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
    window.localStorage.setItem("tally.pending-refund.sale-1", "{}")
    window.localStorage.setItem("other", "keep")
    render(<SignOutButton />)
    fireEvent.click(screen.getByText("退出登录"))
    await waitFor(() => expect(signOut).toHaveBeenCalled())
    expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
    expect(window.localStorage.getItem("tally.pending-refund.sale-1")).toBeNull()
    expect(window.localStorage.getItem("other")).toBe("keep")
    expect(confirmFn).toHaveBeenCalledWith(
      expect.objectContaining({ body: expect.stringContaining("1 笔退款没记上"), confirmText: "仍要退出", cancelText: "先不退出" }),
    )
    expect(signOut).toHaveBeenCalledWith({ redirect: false, redirectTo: "/login?switch=1" })
    expect(document.cookie).toContain("tally-switch=1")
  })

  it("stays signed in and keeps the unbooked refunds when the cashier backs out", async () => {
    confirmFn.mockResolvedValue(false)
    window.localStorage.setItem("tally.pending-refund.sale-1", "{}")
    render(<SignOutButton />)
    fireEvent.click(screen.getByText("退出登录"))
    await waitFor(() => expect(confirmFn).toHaveBeenCalled())
    expect(window.localStorage.getItem("tally.pending-refund.sale-1")).toBe("{}")
    expect(signOut).not.toHaveBeenCalled()
  })

  it("clears a leftover switch marker on mount", () => {
    document.cookie = "tally-switch=1; path=/"
    render(<SignOutButton />)
    expect(document.cookie).not.toContain("tally-switch=1")
  })

  it("drops the cart and goes to the switch login when another tab signs out", () => {
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
      render(<SignOutButton />)
      window.dispatchEvent(new StorageEvent("storage", { key: "other", newValue: "1" }))
      expect(assign).not.toHaveBeenCalled()
      window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: "123" }))
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
      expect(assign).toHaveBeenCalledWith("/login?switch=1")
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })

  it("another tab's sign-out also stops this tab from saving the cart again", () => {
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      render(<SignOutButton />)
      window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: "123" }))
      saveCart(cartState)
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
    } finally {
      resetSigningOut()
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })

  it("leaves when the page is restored from the back/forward cache after a sign-out", () => {
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      render(<SignOutButton />)
      expect(assign).not.toHaveBeenCalled()
      window.localStorage.setItem("tally:signed-out-at", String(Date.now() + 1000))
      window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: false }))
      expect(assign).not.toHaveBeenCalled()
      window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true }))
      expect(assign).toHaveBeenCalledWith("/login?switch=1")
    } finally {
      resetSigningOut()
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })

  it("ignores the signal key being deleted by another tab", () => {
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
      render(<SignOutButton />)
      window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: null }))
      expect(assign).not.toHaveBeenCalled()
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBe("{}")
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })

  it("signals other tabs on sign-out", async () => {
    confirmFn.mockResolvedValue(true)
    render(<SignOutButton />)
    fireEvent.click(screen.getByText("退出登录"))
    await waitFor(() => expect(signOut).toHaveBeenCalled())
    expect(window.localStorage.getItem("tally:signed-out-at")).not.toBeNull()
  })

  it("on mount, drops the cart and leaves when a sign-out happened after this page loaded", () => {
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
      window.localStorage.setItem("tally:signed-out-at", String(Date.now() + 1000))
      render(<SignOutButton />)
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
      expect(assign).toHaveBeenCalledWith("/login?switch=1")
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })

  it("on mount, ignores a sign-out that happened before this page loaded", () => {
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
      window.localStorage.setItem("tally:signed-out-at", "1000")
      render(<SignOutButton />)
      expect(assign).not.toHaveBeenCalled()
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBe("{}")
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })
})
