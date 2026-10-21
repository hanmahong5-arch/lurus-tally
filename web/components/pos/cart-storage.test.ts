import { describe, it, expect, beforeEach } from "vitest"
import Decimal from "decimal.js"
import { saveCart, loadCart, clearCart, markSigningOut, resetSigningOut } from "./cart-storage"
import type { CartState } from "@/lib/pos/cart-reducer"

const state: CartState = {
  items: [
    {
      productId: "p1",
      productName: "不锈钢螺丝",
      unitId: "u1",
      unitName: "个",
      unitPrice: new Decimal("12.5"),
      quantity: new Decimal("3"),
      measurementStrategy: "individual",
    },
  ],
  discount: new Decimal("2"),
  discountType: "fixed",
  remark: "",
}

describe("cart-storage", () => {
  beforeEach(() => {
    window.sessionStorage.clear()
    window.localStorage.clear()
  })

  it("TestCartStorage_Load_DropsCartSavedBeforeLastSignOut", () => {
    saveCart(state)
    window.localStorage.setItem("tally:signed-out-at", String(Date.now() + 1000))
    expect(loadCart()).toBeNull()
    expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
  })

  it("TestCartStorage_Save_NoopWhileSigningOutAndResumesAfterReset", () => {
    try {
      markSigningOut()
      saveCart(state)
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
      resetSigningOut()
      saveCart(state)
      expect(window.sessionStorage.getItem("tally.pos.cart.v1")).not.toBeNull()
    } finally {
      resetSigningOut()
    }
  })

  it("TestCartStorage_Load_KeepsCartSavedAfterLastSignOut", () => {
    window.localStorage.setItem("tally:signed-out-at", String(Date.now() - 1000))
    saveCart(state)
    expect(loadCart()).not.toBeNull()
  })

  it("TestCartStorage_RoundTrip_KeepsDecimals", () => {
    saveCart(state)
    const back = loadCart()!
    expect(back.items[0].unitPrice.toString()).toBe("12.5")
    expect(back.items[0].quantity.toString()).toBe("3")
    expect(back.discount.toString()).toBe("2")
    expect(back.items[0].productName).toBe("不锈钢螺丝")
  })

  it("TestCartStorage_EmptyCart_ClearsStorage", () => {
    saveCart(state)
    saveCart({ ...state, items: [] })
    expect(loadCart()).toBeNull()
  })

  it("TestCartStorage_Garbage_ReturnsNull", () => {
    window.sessionStorage.setItem("tally.pos.cart.v1", "{not json")
    expect(loadCart()).toBeNull()
  })

  it("TestCartStorage_Clear", () => {
    saveCart(state)
    clearCart()
    expect(loadCart()).toBeNull()
  })
})

describe("clearDeviceTraces", () => {
  it("removes the cart and every pending refund but leaves other keys", async () => {
    const { clearDeviceTraces } = await import("./cart-storage")
    window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
    window.localStorage.setItem("tally.pending-refund.a", "{}")
    window.localStorage.setItem("tally.pending-refund.b", "{}")
    window.localStorage.setItem("keep", "1")
    window.localStorage.setItem("tally_ai_history", "[]")
    window.sessionStorage.setItem("tally:copy-bill:sale", "{}")
    clearDeviceTraces()
    expect(window.localStorage.getItem("tally_ai_history")).toBeNull()
    expect(window.sessionStorage.getItem("tally:copy-bill:sale")).toBeNull()
    expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull()
    expect(window.localStorage.getItem("tally.pending-refund.a")).toBeNull()
    expect(window.localStorage.getItem("tally.pending-refund.b")).toBeNull()
    expect(window.localStorage.getItem("keep")).toBe("1")
  })
})
