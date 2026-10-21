import { describe, it, expect } from "vitest"
import Decimal from "decimal.js"
import {
  cartReducer,
  cartTotal,
  cartNetTotal,
  initialCartState,
} from "./cart-reducer"

const makeProduct = (overrides?: Partial<{ id: string; name: string; price: string }>) => ({
  productId: overrides?.id ?? "p1",
  productName: overrides?.name ?? "Test Product",
  unitId: "u1",
  unitName: "pcs",
  unitPrice: new Decimal(overrides?.price ?? "10.00"),
  quantity: new Decimal("1"),
  measurementStrategy: "individual" as const,
})

describe("cartReducer", () => {
  it("TestCartReducer_AddItem_IncreasesQuantity: adding same product twice yields quantity 2", () => {
    const item = makeProduct()
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, { type: "ADD_ITEM", item })
    expect(state.items).toHaveLength(1)
    expect(state.items[0].quantity.toNumber()).toBe(2)
  })

  it("TestCartReducer_AddItem_DifferentProducts_AddsBothRows", () => {
    const a = makeProduct({ id: "p1", name: "A" })
    const b = makeProduct({ id: "p2", name: "B" })
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item: a })
    state = cartReducer(state, { type: "ADD_ITEM", item: b })
    expect(state.items).toHaveLength(2)
  })

  it("TestCartReducer_RemoveItem_DeletesRow: removing the only item leaves empty cart", () => {
    const item = makeProduct()
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, { type: "REMOVE_ITEM", productId: "p1" })
    expect(state.items).toHaveLength(0)
  })

  it("TestCartReducer_SetQuantity_Zero_RemovesItem: quantity 0 removes row", () => {
    const item = makeProduct()
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, { type: "SET_QUANTITY", productId: "p1", quantity: new Decimal(0) })
    expect(state.items).toHaveLength(0)
  })

  it("TestCartReducer_SetQuantity_Positive_UpdatesQty", () => {
    const item = makeProduct()
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, { type: "SET_QUANTITY", productId: "p1", quantity: new Decimal(5) })
    expect(state.items[0].quantity.toNumber()).toBe(5)
  })

  it("TestCartReducer_SetUnitPrice_UpdatesPrice", () => {
    const item = makeProduct({ price: "10.00" })
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, {
      type: "SET_UNIT_PRICE",
      productId: "p1",
      unitPrice: new Decimal("15.50"),
    })
    expect(state.items[0].unitPrice.toFixed(2)).toBe("15.50")
  })

  it("TestCartReducer_ApplyDiscount_StoresDiscount", () => {
    const state = cartReducer(initialCartState, {
      type: "APPLY_DISCOUNT",
      amount: new Decimal("5.00"),
      discountType: "fixed",
    })
    expect(state.discount.toFixed(2)).toBe("5.00")
    expect(state.discountType).toBe("fixed")
  })

  it("TestCartReducer_ClearCart_EmptiesItems", () => {
    const item = makeProduct()
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, { type: "CLEAR_CART" })
    expect(state.items).toHaveLength(0)
    expect(state.discount.toNumber()).toBe(0)
  })

  it("TestCartReducer_Total_UsesDecimalJs: two 0.1-price items sum to exactly 0.20", () => {
    const item = makeProduct({ id: "p1", price: "0.10" })
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, { type: "ADD_ITEM", item })
    const total = cartTotal(state.items)
    // Must NOT be 0.2000000000001 (float64 error)
    expect(total.toFixed(2)).toBe("0.20")
    expect(total.toNumber()).toBe(0.2)
  })

  it("TestCartReducer_Total_WithFixedDiscount_Correct", () => {
    const item = makeProduct({ price: "100.00" })
    let state = cartReducer(initialCartState, { type: "ADD_ITEM", item })
    state = cartReducer(state, {
      type: "APPLY_DISCOUNT",
      amount: new Decimal("10.00"),
      discountType: "fixed",
    })
    const total = cartTotal(state.items)
    expect(total.toFixed(2)).toBe("100.00")
  })
})

describe("cartTotal", () => {
  it("TestCartTotal_EmptyItems_ReturnsZero", () => {
    expect(cartTotal([]).toNumber()).toBe(0)
  })

  it("TestCartTotal_MultipleItems_SumsCorrectly", () => {
    const items = [
      { ...makeProduct({ id: "p1", price: "9.99" }), quantity: new Decimal(2) },
      { ...makeProduct({ id: "p2", price: "4.50" }), quantity: new Decimal(3) },
    ]
    // 2*9.99 + 3*4.50 = 19.98 + 13.50 = 33.48
    expect(cartTotal(items).toFixed(2)).toBe("33.48")
  })

  it("TestCartTotal_TwoLines1p5x3p33_EachLineToCentThenSum", () => {
    const items = [
      { ...makeProduct({ id: "p1", price: "1.5" }), quantity: new Decimal("3.33") },
      { ...makeProduct({ id: "p2", price: "1.5" }), quantity: new Decimal("3.33") },
    ]
    // 4.995 -> 5.00 per line, so 10.00 (not 9.99)
    expect(cartTotal(items).toFixed(2)).toBe("10.00")
    const state = { ...initialCartState, items }
    expect(cartNetTotal(state).toFixed(2)).toBe("10.00")
    expect(Decimal.max(cartTotal(items).minus(cartNetTotal(state).toDecimalPlaces(2)), 0).toFixed(2)).toBe("0.00")
  })
})
