import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { ApiError } from "@/lib/api/errors"

vi.mock("@/lib/api/payment", () => ({
  recordPayment: vi.fn(),
  PAY_TYPE_LABEL: { cash: "现金", wechat: "微信", credit: "赊账", transfer: "转账" },
}))

import { recordPayment } from "@/lib/api/payment"
import { PaymentForm } from "./payment-form"

const setup = (extra: Partial<React.ComponentProps<typeof PaymentForm>> = {}) => {
  const onSuccess = vi.fn()
  render(<PaymentForm billId="b-1" outstandingAmount="100" payments={[]} onSuccess={onSuccess} {...extra} />)
  return onSuccess
}

describe("PaymentForm", () => {
  beforeEach(() => vi.clearAllMocks())

  it("saves the payment, then hands control back so the page can re-read the bill", async () => {
    vi.mocked(recordPayment).mockResolvedValue({} as never)
    const onSuccess = setup()
    fireEvent.change(screen.getByLabelText("金额"), { target: { value: "40.5" } })
    fireEvent.click(screen.getByText("确认收款"))
    await waitFor(() => expect(onSuccess).toHaveBeenCalledTimes(1))
    expect(recordPayment).toHaveBeenCalledWith(
      { bill_id: "b-1", amount: "40.5", payment_method: "cash", remark: undefined },
      undefined,
    )
    expect(onSuccess).toHaveBeenCalledWith()
  })

  it("never offers credit as a way of paying", () => {
    setup()
    expect(screen.queryByText("赊账")).toBeNull()
  })

  it("refuses more than is outstanding without calling the server", () => {
    setup()
    fireEvent.change(screen.getByLabelText("金额"), { target: { value: "150" } })
    fireEvent.click(screen.getByText("确认收款"))
    expect(screen.getByText("本次 ¥150.00 超过待收 ¥100.00")).toBeInTheDocument()
    expect(recordPayment).not.toHaveBeenCalled()
  })

  it("shows the Chinese reason from the server without a class-name prefix", async () => {
    vi.mocked(recordPayment).mockRejectedValue(
      new ApiError(422, "payment_exceeds_outstanding", "HTTP 422", { message: "本次 90 超过待收 50" }),
    )
    const onSuccess = setup()
    fireEvent.change(screen.getByLabelText("金额"), { target: { value: "90" } })
    fireEvent.click(screen.getByText("确认收款"))
    expect(await screen.findByText("本次 90 超过待收 50")).toBeInTheDocument()
    expect(document.body.textContent).not.toContain("ApiError")
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it("uses payable wording for purchases", () => {
    setup({ kind: "pay" })
    expect(screen.getByText("待付金额")).toBeInTheDocument()
    expect(screen.getByText("确认付款")).toBeInTheDocument()
  })

  it("says the bill is settled once nothing is outstanding", () => {
    setup({
      outstandingAmount: "0",
      payments: [{ id: "p", bill_id: "b-1", amount: "100", pay_type: "cash", pay_date: "2026-01-02T00:00:00Z" }],
    })
    expect(screen.getByText("该销售单已结清")).toBeInTheDocument()
  })

  it("pays a legacy four-decimal balance in full with the amount to the cent", async () => {
    vi.mocked(recordPayment).mockResolvedValue({} as never)
    const onSuccess = setup({ outstandingAmount: "41.625" })
    fireEvent.click(screen.getByText("全额"))
    expect(screen.getByLabelText("金额")).toHaveValue(41.63)
    fireEvent.click(screen.getByText("确认收款"))
    await waitFor(() => expect(onSuccess).toHaveBeenCalledTimes(1))
    expect(recordPayment).toHaveBeenCalledWith(
      { bill_id: "b-1", amount: "41.63", payment_method: "cash", remark: undefined },
      undefined,
    )
  })
})
