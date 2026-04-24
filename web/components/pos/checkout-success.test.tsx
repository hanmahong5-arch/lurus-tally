import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, act } from "@testing-library/react"
import { CheckoutSuccess } from "./checkout-success"

describe("CheckoutSuccess", () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("TestCheckoutSuccess_ShowsBillNo: renders the bill number", () => {
    const onDismiss = vi.fn()
    render(
      <CheckoutSuccess
        billNo="SO-2024-0001"
        totalAmount="99.80"
        onDismiss={onDismiss}
      />
    )
    expect(screen.getByText("SO-2024-0001")).toBeInTheDocument()
  })

  it("TestCheckoutSuccess_ShowsTotalAmount: renders the total amount", () => {
    const onDismiss = vi.fn()
    render(
      <CheckoutSuccess
        billNo="SO-2024-0001"
        totalAmount="99.80"
        onDismiss={onDismiss}
      />
    )
    expect(screen.getByText(/99\.80/)).toBeInTheDocument()
  })

  it("TestCheckoutSuccess_AutoDismiss_After1000ms: calls onDismiss after 1 second", () => {
    const onDismiss = vi.fn()
    render(
      <CheckoutSuccess
        billNo="SO-2024-0001"
        totalAmount="99.80"
        onDismiss={onDismiss}
      />
    )

    expect(onDismiss).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(1000)
    })

    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it("TestCheckoutSuccess_ManualClose_CallsOnDismiss: clicking close button immediately dismisses", () => {
    const onDismiss = vi.fn()
    render(
      <CheckoutSuccess
        billNo="SO-2024-0001"
        totalAmount="99.80"
        onDismiss={onDismiss}
      />
    )

    fireEvent.click(screen.getByRole("button", { name: /关闭/ }))
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it("TestCheckoutSuccess_ClearsTimer_OnUnmount: timer is cleaned up on unmount", () => {
    const onDismiss = vi.fn()
    const { unmount } = render(
      <CheckoutSuccess
        billNo="SO-2024-0001"
        totalAmount="99.80"
        onDismiss={onDismiss}
      />
    )

    unmount()

    act(() => {
      vi.advanceTimersByTime(2000)
    })

    // onDismiss should NOT have been called after unmount
    expect(onDismiss).not.toHaveBeenCalled()
  })
})
