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
  describe("with a bill id (receipt printing)", () => {
    const props = { billNo: "SO-1", totalAmount: "9.90", billId: "bill-1" }

    beforeEach(() => {
      window.localStorage.removeItem("tally-pos-auto-print")
    })

    it("TestCheckoutSuccess_PrintButton_Shown: offers 打印小票 and no frame yet", () => {
      render(<CheckoutSuccess {...props} onDismiss={vi.fn()} />)
      expect(screen.getByRole("button", { name: "打印小票" })).toBeInTheDocument()
      expect(document.querySelector("iframe")).toBeNull()
    })

    it("TestCheckoutSuccess_NoDismissAt1s_WhenPrintable: waits longer than the plain flash", () => {
      const onDismiss = vi.fn()
      render(<CheckoutSuccess {...props} onDismiss={onDismiss} />)
      act(() => {
        vi.advanceTimersByTime(1000)
      })
      expect(onDismiss).not.toHaveBeenCalled()
    })

    it("TestCheckoutSuccess_Enter_PrintsReceipt: Enter mounts the print frame for this bill", () => {
      render(<CheckoutSuccess {...props} onDismiss={vi.fn()} />)
      act(() => {
        vi.advanceTimersByTime(2000)
      })
      fireEvent.keyDown(window, { key: "Enter" })
      const frame = document.querySelector("iframe")
      expect(frame?.getAttribute("src")).toBe("/print/pos/bill-1?auto=1")
    })

    it("TestCheckoutSuccess_ScannerEnterWithin2s_DoesNotPrint: a scan right after checkout ends with Enter", () => {
      render(<CheckoutSuccess {...props} onDismiss={vi.fn()} />)
      act(() => {
        vi.advanceTimersByTime(1900)
      })
      fireEvent.keyDown(window, { key: "Enter" })
      expect(document.querySelector("iframe")).toBeNull()
    })

    it("TestCheckoutSuccess_EnterWithoutButtonFocus_DoesNotPrint", () => {
      render(<CheckoutSuccess {...props} onDismiss={vi.fn()} />)
      act(() => {
        vi.advanceTimersByTime(3000)
      })
      ;(document.activeElement as HTMLElement | null)?.blur()
      fireEvent.keyDown(window, { key: "Enter" })
      expect(document.querySelector("iframe")).toBeNull()
    })

    it("TestCheckoutSuccess_ScannerBurstAt3s_DismissesAndDoesNotPrint: digits then Enter is the next customer's scan", () => {
      const onDismiss = vi.fn()
      render(<CheckoutSuccess {...props} onDismiss={onDismiss} />)
      act(() => {
        vi.advanceTimersByTime(3000)
      })
      for (const k of ["6", "9", "0"]) fireEvent.keyDown(window, { key: k })
      fireEvent.keyDown(window, { key: "Enter" })
      expect(onDismiss).toHaveBeenCalled()
      expect(document.querySelector("iframe")).toBeNull()
    })

    it("TestCheckoutSuccess_CreditSale_SaysOwed: shows 已记赊账 instead of 已收款", () => {
      render(<CheckoutSuccess {...props} owedAmount="9.9" onDismiss={vi.fn()} />)
      expect(screen.getByText("已记赊账，客户欠 ¥9.90")).toBeInTheDocument()
      expect(screen.queryByText("已收款")).toBeNull()
    })

    it("TestCheckoutSuccess_CashSale_KeepsTenderForTheReceipt", () => {
      render(<CheckoutSuccess {...props} receivedAmount="100.00" changeAmount="90.10" onDismiss={vi.fn()} />)
      expect(JSON.parse(window.localStorage.getItem("tally-pos-tender:bill-1") ?? "null")).toMatchObject({
        received: "100.00",
        change: "90.10",
      })
    })

    it("TestCheckoutSuccess_Escape_Dismisses", () => {
      const onDismiss = vi.fn()
      render(<CheckoutSuccess {...props} onDismiss={onDismiss} />)
      fireEvent.keyDown(window, { key: "Escape" })
      expect(onDismiss).toHaveBeenCalledTimes(1)
    })

    it("TestCheckoutSuccess_PrintDoneMessage_Dismisses", () => {
      const onDismiss = vi.fn()
      render(<CheckoutSuccess {...props} onDismiss={onDismiss} />)
      fireEvent(window, new MessageEvent("message", { data: "tally:print-done", origin: window.location.origin }))
      expect(onDismiss).toHaveBeenCalledTimes(1)
    })

    it("TestCheckoutSuccess_PrintIncompleteMessage_TellsWhereToReprint", () => {
      render(<CheckoutSuccess {...props} onDismiss={vi.fn()} />)
      fireEvent(window, new MessageEvent("message", { data: "tally:print-incomplete", origin: window.location.origin }))
      expect(screen.getByRole("alert").textContent).toContain("今日结单")
    })

    it("TestCheckoutSuccess_AutoPrintSetting_PrintsWithoutKey", () => {
      window.localStorage.setItem("tally-pos-auto-print", "1")
      render(<CheckoutSuccess {...props} onDismiss={vi.fn()} />)
      expect(document.querySelector("iframe")?.getAttribute("src")).toBe("/print/pos/bill-1?auto=1")
    })

    it("TestCheckoutSuccess_EnterAt7_9s_KeepsFrameAlive: pressing Enter late must not let the 8 s timer unmount the frame", () => {
      const onDismiss = vi.fn()
      render(<CheckoutSuccess {...props} onDismiss={onDismiss} />)
      act(() => {
        vi.advanceTimersByTime(7900)
      })
      fireEvent.keyDown(window, { key: "Enter" })
      act(() => {
        vi.advanceTimersByTime(200)
      })
      expect(onDismiss).not.toHaveBeenCalled()
      expect(document.querySelector("iframe")).not.toBeNull()
      act(() => {
        vi.advanceTimersByTime(30000)
      })
      expect(onDismiss).toHaveBeenCalledTimes(1)
    })

    it("TestCheckoutSuccess_PrintButtonLate_KeepsFrameAlive", () => {
      const onDismiss = vi.fn()
      render(<CheckoutSuccess {...props} onDismiss={onDismiss} />)
      act(() => {
        vi.advanceTimersByTime(7500)
      })
      fireEvent.click(screen.getByRole("button", { name: "打印小票" }))
      act(() => {
        vi.advanceTimersByTime(1000)
      })
      expect(onDismiss).not.toHaveBeenCalled()
      expect(document.querySelector("iframe")).not.toBeNull()
    })

    it("TestCheckoutSuccess_NoBillId_EnterDoesNothing", () => {
      render(<CheckoutSuccess billNo="SO-1" totalAmount="9.90" onDismiss={vi.fn()} />)
      fireEvent.keyDown(window, { key: "Enter" })
      expect(document.querySelector("iframe")).toBeNull()
    })
  })
})
