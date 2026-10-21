import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const api = vi.hoisted(() => ({ previewReceipt: vi.fn(), createReceipt: vi.fn() }))
vi.mock("@/lib/api/receipts", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/receipts")>()),
  ...api,
}))

import { ReceiptDialog } from "./receipt-dialog"
import { ApiError, NetworkError } from "@/lib/api/errors"

const preview = {
  owed: "600",
  amount: "250",
  allocations: [
    { bill_id: "b1", bill_no: "SL-001", bill_date: "2026-10-01", outstanding: "100", applied: "100", remaining: "0" },
    { bill_id: "b2", bill_no: "SL-002", bill_date: "2026-10-02", outstanding: "200", applied: "150", remaining: "50" },
  ],
}

function renderDialog(onDone = vi.fn()) {
  render(
    <ReceiptDialog open onOpenChange={() => {}} direction="ar" partnerId="c-1" partnerName="老客户" owed="600" onDone={onDone} />
  )
  return onDone
}

describe("ReceiptDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.previewReceipt.mockResolvedValue(preview)
  })

  it("prefills the full balance and lists the bills the money would settle", async () => {
    renderDialog()
    expect(screen.getByLabelText("收到金额")).toHaveValue("600.00")
    fireEvent.change(screen.getByLabelText("收到金额"), { target: { value: "250" } })
    expect(await screen.findByText("SL-002")).toBeInTheDocument()
    expect(screen.getByText("已结清")).toBeInTheDocument()
    expect(screen.getByText("¥50.00")).toBeInTheDocument()
    expect(api.previewReceipt).toHaveBeenLastCalledWith("ar", "c-1", "250", expect.anything())
  })

  it("shows the server's message when the money is more than the debt", async () => {
    api.previewReceipt.mockRejectedValue(new ApiError(422, "receipt_exceeds_debt", "x", { message: "该客户只欠 600.00，本次 700.00 超过了" }))
    renderDialog()
    fireEvent.change(screen.getByLabelText("收到金额"), { target: { value: "700" } })
    expect(await screen.findByRole("alert")).toHaveTextContent("该客户只欠 600.00")
    expect(screen.getByRole("button", { name: "确认收款" })).toBeDisabled()
  })

  it("rejects a malformed amount without calling the server", async () => {
    renderDialog()
    fireEvent.change(screen.getByLabelText("收到金额"), { target: { value: "12.345" } })
    expect(await screen.findByRole("alert")).toHaveTextContent("最多两位小数")
    expect(api.previewReceipt).not.toHaveBeenCalledWith("ar", "c-1", "12.345", expect.anything())
  })

  it("saves once and tells the page to refresh", async () => {
    api.createReceipt.mockResolvedValue(preview)
    const onDone = renderDialog()
    fireEvent.change(screen.getByLabelText("收到金额"), { target: { value: "250" } })
    await screen.findByText("SL-002")
    fireEvent.click(screen.getByRole("button", { name: "确认收款" }))
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))
    expect(api.createReceipt).toHaveBeenCalledTimes(1)
    expect(api.createReceipt.mock.calls[0][0]).toMatchObject({ direction: "ar", partner_id: "c-1", amount: "250", payment_method: "cash" })
  })

  async function saveWithDiff(diff: string) {
    api.createReceipt.mockResolvedValue({ ...preview, booked: preview.amount, diff })
    const onDone = renderDialog()
    fireEvent.change(screen.getByLabelText("收到金额"), { target: { value: "250" } })
    await screen.findByText("SL-002")
    fireEvent.click(screen.getByRole("button", { name: "确认收款" }))
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))
    const { toast } = await import("sonner")
    return String(vi.mocked(toast.success).mock.calls.at(-1)?.[0])
  }

  it("says a positive tail was booked on top", async () => {
    const note = await saveWithDiff("0.01")
    expect(note).toContain("¥0.01 零头，已一并记上")
  })

  it("says a negative tail was left uncollected, without a minus sign", async () => {
    const note = await saveWithDiff("-0.02")
    expect(note).toContain("¥0.02 零头没收，已按结清处理")
    expect(note).not.toContain("-")
  })

  it("does not round a sub-cent tail up to a cent", async () => {
    const note = await saveWithDiff("0.006")
    expect(note).toContain("不足一分钱")
    expect(note).not.toContain("¥0.01")
  })

  it("retries a lost reply with the same idempotency key", async () => {
    api.createReceipt.mockRejectedValueOnce(new NetworkError("offline", "offline")).mockResolvedValueOnce(preview)
    renderDialog()
    fireEvent.change(screen.getByLabelText("收到金额"), { target: { value: "250" } })
    await screen.findByText("SL-002")
    fireEvent.click(screen.getByRole("button", { name: "确认收款" }))
    await screen.findByRole("alert")
    fireEvent.click(screen.getByRole("button", { name: "确认收款" }))
    await waitFor(() => expect(api.createReceipt).toHaveBeenCalledTimes(2))
    const k1 = api.createReceipt.mock.calls[0][2]
    expect(k1).toBeTruthy()
    expect(api.createReceipt.mock.calls[1][2]).toBe(k1)
  })

  it("re-reads the split after the server refuses the save", async () => {
    api.createReceipt.mockRejectedValueOnce(new ApiError(422, "receipt_exceeds_debt", "x", { message: "该客户只欠 100.00，本次 250.00 超过了" }))
    renderDialog()
    fireEvent.change(screen.getByLabelText("收到金额"), { target: { value: "250" } })
    await screen.findByText("SL-002")
    const before = api.previewReceipt.mock.calls.length
    fireEvent.click(screen.getByRole("button", { name: "确认收款" }))
    await waitFor(() => expect(api.previewReceipt.mock.calls.length).toBeGreaterThan(before))
  })
})
