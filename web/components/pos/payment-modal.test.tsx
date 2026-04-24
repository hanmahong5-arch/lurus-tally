import { describe, it, expect, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import Decimal from "decimal.js"
import { PaymentModal } from "./payment-modal"

describe("PaymentModal", () => {
  const baseProps = {
    open: true,
    totalAmount: new Decimal("99.80"),
    onConfirm: vi.fn(),
    onClose: vi.fn(),
  }

  it("TestPaymentModal_CashMode_CalculatesChange: paid 120, total 99.80, change = 20.20", () => {
    render(
      <PaymentModal {...baseProps} mode="cash" />
    )

    const input = screen.getByLabelText(/实收/i)
    fireEvent.change(input, { target: { value: "120" } })

    expect(screen.getByText(/20\.20/)).toBeInTheDocument()
  })

  it("TestPaymentModal_CashMode_NegativeChange_ShowsWarning: paid 50, total 99.80, negative change shows warning", () => {
    render(
      <PaymentModal {...baseProps} mode="cash" />
    )

    const input = screen.getByLabelText(/实收/i)
    fireEvent.change(input, { target: { value: "50" } })

    const changeEl = screen.getByTestId("change-amount")
    expect(changeEl.className).toMatch(/red|destructive|warning/)
  })

  it("TestPaymentModal_Confirm_CallsOnConfirm: Enter key triggers onConfirm with correct payment_method", () => {
    const onConfirm = vi.fn()
    render(
      <PaymentModal {...baseProps} mode="cash" onConfirm={onConfirm} />
    )

    const input = screen.getByLabelText(/实收/i)
    fireEvent.change(input, { target: { value: "100" } })
    fireEvent.keyDown(input, { key: "Enter" })

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ paymentMethod: "cash" })
    )
  })

  it("TestPaymentModal_WechatMode_ShowsQRPlaceholder", () => {
    render(
      <PaymentModal {...baseProps} mode="wechat" />
    )
    expect(screen.getByText(/二维码/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /已收款/ })).toBeInTheDocument()
  })

  it("TestPaymentModal_AlipayMode_ShowsQRPlaceholder", () => {
    render(
      <PaymentModal {...baseProps} mode="alipay" />
    )
    expect(screen.getByText(/二维码/)).toBeInTheDocument()
  })

  it("TestPaymentModal_CreditMode_RequiresCustomerName", () => {
    const onConfirm = vi.fn()
    render(
      <PaymentModal {...baseProps} mode="credit" onConfirm={onConfirm} />
    )
    expect(screen.getByLabelText(/客户姓名/i)).toBeInTheDocument()
  })

  it("TestPaymentModal_WechatConfirm_CallsOnConfirmWithWechat", () => {
    const onConfirm = vi.fn()
    render(
      <PaymentModal {...baseProps} mode="wechat" onConfirm={onConfirm} />
    )

    fireEvent.click(screen.getByRole("button", { name: /已收款/ }))

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ paymentMethod: "wechat" })
    )
  })
})
