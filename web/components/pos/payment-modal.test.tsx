import { describe, it, expect, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import Decimal from "decimal.js"
import { PaymentModal } from "./payment-modal"

vi.mock("@/components/entity-pickers", () => ({
  CustomerPicker: (props: { onSelect: (o: { id: string; name: string }) => void }) => (
    <button type="button" onClick={() => props.onSelect({ id: "cust-1", name: "老王" })}>
      选中老王
    </button>
  ),
}))

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

  it("TestPaymentModal_WechatMode_NoPlaceholder_ConfirmsReceived", () => {
    render(
      <PaymentModal {...baseProps} mode="wechat" />
    )
    expect(screen.queryByText(/占位/)).not.toBeInTheDocument()
    expect(screen.queryByText(/二维码/)).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: /已收到顾客付款/ })).toBeInTheDocument()
  })

  it("TestPaymentModal_AlipayMode_NoPlaceholder", () => {
    render(
      <PaymentModal {...baseProps} mode="alipay" />
    )
    expect(screen.queryByText(/占位/)).not.toBeInTheDocument()
    expect(screen.getByLabelText(/流水尾号/)).toBeInTheDocument()
  })

  it("TestPaymentModal_WechatConfirm_CallsOnConfirmWithWechat", () => {
    const onConfirm = vi.fn()
    render(
      <PaymentModal {...baseProps} mode="wechat" onConfirm={onConfirm} />
    )

    fireEvent.click(screen.getByRole("button", { name: /已收到顾客付款/ }))

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ paymentMethod: "wechat" })
    )
    expect(onConfirm.mock.calls[0][0].note).toBeUndefined()
  })

  it("TestPaymentModal_WechatConfirm_TailGoesToNote", () => {
    const onConfirm = vi.fn()
    render(
      <PaymentModal {...baseProps} mode="wechat" onConfirm={onConfirm} />
    )

    fireEvent.change(screen.getByLabelText(/流水尾号/), { target: { value: "5821" } })
    fireEvent.click(screen.getByRole("button", { name: /已收到顾客付款/ }))

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ paymentMethod: "wechat", note: "微信流水尾号 5821" })
    )
  })

  it("TestPaymentModal_Credit_BlockedUntilCustomerPicked", () => {
    const onConfirm = vi.fn()
    render(
      <PaymentModal {...baseProps} mode="credit" onConfirm={onConfirm} />
    )
    const confirm = screen.getByRole("button", { name: /确认赊账/ })
    expect(confirm).toBeDisabled()
    expect(screen.getByText(/赊账必须选择客户/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "选中老王" }))
    expect(confirm).not.toBeDisabled()
    fireEvent.click(confirm)

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ paymentMethod: "credit", partnerId: "cust-1" })
    )
  })
})
