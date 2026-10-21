import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"

const { signOut } = vi.hoisted(() => ({ signOut: vi.fn() }))
vi.mock("next-auth/react", () => ({ signOut }))
const { toastError } = vi.hoisted(() => ({ toastError: vi.fn() }))
vi.mock("sonner", () => ({ toast: { error: toastError } }))

import { signOutForSwitch } from "./switch-user"
import { saveCart, resetSigningOut } from "@/components/pos/cart-storage"
import Decimal from "decimal.js"

describe("signOutForSwitch copy", () => {
  const orig = window.location
  const assign = vi.fn()
  beforeEach(() => {
    assign.mockReset()
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    signOut.mockClear()
    document.cookie = "tally-switch=; path=/; max-age=0"
    window.localStorage.clear()
    window.localStorage.setItem("tally.pending-refund.sale-1", "{}")
    resetSigningOut()
  })

  afterEach(() => {
    Object.defineProperty(window, "location", { configurable: true, value: orig })
  })

  it("tells a cashier to ask the boss or manager", async () => {
    const confirm = vi.fn().mockResolvedValue(false)
    await signOutForSwitch(confirm, "cashier")
    expect(confirm.mock.calls[0][0].body).toContain("告诉老板或店长")
  })

  it("tells an owner to book it themselves", async () => {
    const confirm = vi.fn().mockResolvedValue(false)
    await signOutForSwitch(confirm, "owner")
    expect(confirm.mock.calls[0][0].body).toContain("请先补记")
    expect(confirm.mock.calls[0][0].body).not.toContain("告诉老板")
  })

  it("returns to the given path after the next sign-in, keeping the whole invite link", async () => {
    window.localStorage.clear()
    await signOutForSwitch(vi.fn(), "owner", "/join?token=abc&shop=%E5%BA%97")
    expect(signOut).toHaveBeenCalledWith({
      redirect: false,
      redirectTo: "/login?switch=1&callbackUrl=" + encodeURIComponent("/join?token=abc&shop=%E5%BA%97"),
    })
  })

  it("ignores a return address that leaves the site", async () => {
    window.localStorage.clear()
    await signOutForSwitch(vi.fn(), "owner", "//evil.example/x")
    expect(signOut).toHaveBeenCalledWith({ redirect: false, redirectTo: "/login?switch=1" })
  })

  it("keeps the shared-till marker for 30 days", async () => {
    window.localStorage.clear()
    await signOutForSwitch(vi.fn(), "owner")
    expect(document.cookie).toContain("tally-switch=1")
    const spy = vi.spyOn(document, "cookie", "set")
    await signOutForSwitch(vi.fn(), "owner")
    expect(spy.mock.calls.some((c) => String(c[0]).includes(`max-age=${30 * 24 * 60 * 60}`))).toBe(true)
    spy.mockRestore()
  })

  it.each([
    [null, "请先补记，或告诉老板、店长"],
    [undefined, "请先补记，或告诉老板、店长"],
    ["clerk" as const, "请先补记，或告诉老板、店长"],
    ["finance" as const, "请先补记，或告诉老板、店长"],
  ])("uses the neutral copy for role %s", async (role, text) => {
    const confirm = vi.fn().mockResolvedValue(false)
    await signOutForSwitch(confirm, role)
    expect(confirm.mock.calls[0][0].body).toContain(text)
  })

  it("does not write the cart back once the sign-out has started", async () => {
    window.localStorage.clear()
    window.sessionStorage.clear()
    const state = {
      items: [{ productId: "p", productName: "n", unitId: "u", unitName: "个", unitPrice: new Decimal(1), quantity: new Decimal(1), measurementStrategy: "individual" as const }],
      discount: new Decimal(0),
      discountType: "fixed" as const,
      remark: "",
    }
    try {
      await signOutForSwitch(vi.fn(), "cashier")
      saveCart(state)
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
    } finally {
      resetSigningOut()
    }
  })

  it("lifts the sign-out mark and tells the user when the sign-out call fails", async () => {
    window.localStorage.clear()
    window.sessionStorage.clear()
    toastError.mockClear()
    signOut.mockRejectedValueOnce(new Error("offline"))
    const state = {
      items: [{ productId: "p", productName: "n", unitId: "u", unitName: "个", unitPrice: new Decimal(1), quantity: new Decimal(1), measurementStrategy: "individual" as const }],
      discount: new Decimal(0),
      discountType: "fixed" as const,
      remark: "",
    }
    try {
      await expect(signOutForSwitch(vi.fn(), "cashier")).resolves.toBeUndefined()
      expect(toastError).toHaveBeenCalledWith(expect.stringContaining("退出没有成功"))
      expect(document.cookie).not.toContain("tally-switch=1")
      saveCart(state)
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).not.toBeNull()
    } finally {
      resetSigningOut()
    }
  })

  it("keeps everything as it was when the sign-out call throws", async () => {
    window.localStorage.clear()
    window.sessionStorage.clear()
    window.sessionStorage.setItem("tally.pos.cart.v1", JSON.stringify({ items: [{ productId: "p" }], savedAt: 1 }))
    window.localStorage.setItem("tally.pending-refund.sale-1", "{\"saleId\":\"sale-1\"}")
    window.localStorage.setItem("tally:signed-out-at", "1000")
    signOut.mockRejectedValueOnce(new Error("offline"))
    const failed = vi.fn()
    window.addEventListener("tally:signout-failed", failed)
    try {
      await signOutForSwitch(vi.fn().mockResolvedValue(true), "cashier")
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toContain("\"productId\":\"p\"")
      expect(window.localStorage.getItem("tally.pending-refund.sale-1")).toBe("{\"saleId\":\"sale-1\"}")
      expect(window.localStorage.getItem("tally:signed-out-at")).toBe("1000")
      expect(assign).not.toHaveBeenCalled()
      expect(failed).toHaveBeenCalledTimes(1)
    } finally {
      window.removeEventListener("tally:signout-failed", failed)
      resetSigningOut()
    }
  })

  it("wipes traces, stamps the time and navigates only after the sign-out succeeded", async () => {
    window.localStorage.clear()
    window.sessionStorage.clear()
    window.sessionStorage.setItem("tally.pos.cart.v1", JSON.stringify({ items: [{ productId: "p" }], savedAt: 1 }))
    window.localStorage.setItem("tally.pending-refund.sale-1", "{}")
    let release!: () => void
    signOut.mockImplementationOnce(() => new Promise<void>((r) => (release = r)))
    try {
      const p = signOutForSwitch(vi.fn().mockResolvedValue(true), "cashier")
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
      expect(window.localStorage.getItem("tally:signed-out-at")).toBeNull()
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).not.toBeNull()
      expect(assign).not.toHaveBeenCalled()
      release()
      await p
      expect(window.localStorage.getItem("tally:signed-out-at")).not.toBeNull()
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
      expect(window.localStorage.getItem("tally.pending-refund.sale-1")).toBeNull()
      expect(assign).toHaveBeenCalledWith("/login?switch=1")
    } finally {
      resetSigningOut()
    }
  })
})
