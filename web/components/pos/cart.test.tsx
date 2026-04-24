import { describe, it, expect, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import Decimal from "decimal.js"
import { Cart } from "./cart"
import type { CartState } from "@/lib/pos/cart-reducer"
import { initialCartState } from "@/lib/pos/cart-reducer"

const makeState = (overrides?: Partial<CartState>): CartState => ({
  ...initialCartState,
  ...overrides,
})

const sampleItem = {
  productId: "p1",
  productName: "Bolt M8",
  unitId: "u1",
  unitName: "pcs",
  unitPrice: new Decimal("9.99"),
  quantity: new Decimal("2"),
  measurementStrategy: "individual" as const,
}

describe("Cart", () => {
  it("TestCart_EmptyCart_ShowsPlaceholder: empty items renders empty state message", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    render(
      <Cart
        state={makeState()}
        dispatch={dispatch}
        onCheckout={onCheckout}
      />
    )
    expect(screen.getByText(/购物车为空/)).toBeInTheDocument()
  })

  it("TestCart_WithItems_ShowsProductName: renders product name in cart", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    const state = makeState({ items: [sampleItem] })

    render(<Cart state={state} dispatch={dispatch} onCheckout={onCheckout} />)

    expect(screen.getByText("Bolt M8")).toBeInTheDocument()
  })

  it("TestCart_AdjustQuantity_UpdatesTotal: clicking + dispatches SET_QUANTITY with incremented qty", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    const state = makeState({ items: [sampleItem] })

    render(<Cart state={state} dispatch={dispatch} onCheckout={onCheckout} />)

    const addButtons = screen.getAllByText("+")
    fireEvent.click(addButtons[0])

    expect(dispatch).toHaveBeenCalledWith({
      type: "SET_QUANTITY",
      productId: "p1",
      quantity: new Decimal("3"),
    })
  })

  it("TestCart_DecreaseQuantity_DispatchesSetQuantity: clicking - dispatches SET_QUANTITY with decremented qty", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    const state = makeState({ items: [sampleItem] })

    render(<Cart state={state} dispatch={dispatch} onCheckout={onCheckout} />)

    const minusButtons = screen.getAllByText("−")
    fireEvent.click(minusButtons[0])

    expect(dispatch).toHaveBeenCalledWith({
      type: "SET_QUANTITY",
      productId: "p1",
      quantity: new Decimal("1"),
    })
  })

  it("TestCart_RemoveItem_DispatchesRemoveItem", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    const state = makeState({ items: [sampleItem] })

    render(<Cart state={state} dispatch={dispatch} onCheckout={onCheckout} />)

    const removeBtn = screen.getByLabelText(/删除/)
    fireEvent.click(removeBtn)

    expect(dispatch).toHaveBeenCalledWith({
      type: "REMOVE_ITEM",
      productId: "p1",
    })
  })

  it("TestCart_ShowsTotal_WithDecimalPrecision: shows correct total in summary row", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    const state = makeState({ items: [sampleItem] })
    // 2 * 9.99 = 19.98

    render(<Cart state={state} dispatch={dispatch} onCheckout={onCheckout} />)

    // The total summary div has the large bold display
    const totalEls = screen.getAllByText(/¥19\.98/)
    expect(totalEls.length).toBeGreaterThanOrEqual(1)
  })

  it("TestCart_EmptyCart_CheckoutButtonDisabled: checkout button disabled when cart is empty", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()

    render(
      <Cart state={makeState()} dispatch={dispatch} onCheckout={onCheckout} />
    )

    const checkoutBtn = screen.getByRole("button", { name: /现金/ })
    expect(checkoutBtn).toBeDisabled()
  })

  it("TestCart_WithItems_CheckoutButtonEnabled", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    const state = makeState({ items: [sampleItem] })

    render(<Cart state={state} dispatch={dispatch} onCheckout={onCheckout} />)

    const cashBtn = screen.getByRole("button", { name: /现金/ })
    expect(cashBtn).not.toBeDisabled()
  })

  it("TestCart_CashCheckout_CallsOnCheckoutWithCash: clicking cash button calls onCheckout('cash')", () => {
    const dispatch = vi.fn()
    const onCheckout = vi.fn()
    const state = makeState({ items: [sampleItem] })

    render(<Cart state={state} dispatch={dispatch} onCheckout={onCheckout} />)

    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    expect(onCheckout).toHaveBeenCalledWith("cash")
  })
})
