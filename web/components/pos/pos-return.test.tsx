import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { ApiError } from "@/lib/api/errors"

vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "owner", isOwner: true } } }),
}))
vi.mock("@/lib/api/sale", () => ({
  listSaleBills: vi.fn(),
  getSaleBill: vi.fn(),
  returnSaleBill: vi.fn(),
}))
vi.mock("@/hooks/use-product-names", () => ({ useProductNames: () => ({ "p-1": "螺丝" }) }))

import { listSaleBills, getSaleBill, returnSaleBill } from "@/lib/api/sale"
import { PosReturn } from "./pos-return"

const bill = (id: string, no: string) => ({
  id,
  bill_no: no,
  status: 2,
  bill_date: "2026-10-10T08:00:00Z",
  total_amount: "100",
  paid_amount: "100",
})
const detail = {
  head: bill("s-1", "SL-20261010-0001"),
  items: [{ id: "line-1", product_id: "p-1", line_no: 1, qty: "5", unit_price: "20", line_amount: "100", unit_name: "个" }],
  payments: [{ id: "pay-1", pay_type: "cash", amount: "100" }],
  returned_amount: "0",
} as never

function setup(onDone = vi.fn(), onOpenChange = vi.fn()) {
  render(<PosReturn open onOpenChange={onOpenChange} tenantId="t1" onDone={onDone} />)
  return { onDone, onOpenChange }
}

describe("PosReturn", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(listSaleBills).mockResolvedValue({
      items: [bill("s-1", "SL-20261010-0001"), bill("s-2", "SL-20261010-0002")] as never,
      total: 2,
    })
    vi.mocked(getSaleBill).mockResolvedValue(detail)
  })

  it("lists the latest 20 approved sales on open", async () => {
    setup()
    expect(await screen.findByText("SL-20261010-0001")).toBeInTheDocument()
    expect(screen.getByText("SL-20261010-0002")).toBeInTheDocument()
    expect(listSaleBills).toHaveBeenCalledWith(expect.objectContaining({ size: 20, status: 2, q: undefined, tenantId: "t1" }))
  })

  it("finds a receipt by its number", async () => {
    setup()
    await screen.findByText("SL-20261010-0001")
    fireEvent.change(screen.getByLabelText("小票单号"), { target: { value: "0002" } })
    fireEvent.click(screen.getByText("查找"))
    await waitFor(() => expect(listSaleBills).toHaveBeenLastCalledWith(expect.objectContaining({ q: "0002" })))
  })

  it("returns goods of the picked receipt with a cash refund and reports the result", async () => {
    vi.mocked(returnSaleBill).mockResolvedValue({
      bill_id: "r-1",
      bill_no: "RT-1",
      return_amount: "40",
      refunded_amount: "40",
    })
    const { onDone, onOpenChange } = setup()
    fireEvent.click(await screen.findByText("SL-20261010-0001"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    expect(screen.getByLabelText("退款方式")).toHaveValue("cash")
    fireEvent.click(screen.getByText("确认退货"))

    await waitFor(() => expect(returnSaleBill).toHaveBeenCalled())
    expect(returnSaleBill).toHaveBeenCalledWith(
      "s-1",
      expect.objectContaining({
        items: [{ bill_item_id: "line-1", qty: "2" }],
        refund_amount: "40.00",
        refund_method: "cash",
      }),
      "t1",
      expect.any(String)
    )
    await waitFor(() => expect(onDone).toHaveBeenCalledWith(expect.stringContaining("已退款")))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it("keeps the dialog open and shows the Chinese reason when the server refuses", async () => {
    vi.mocked(returnSaleBill).mockRejectedValue(
      new ApiError(422, "refund_exceeds_paid", "HTTP 422", { error: "refund_exceeds_paid", message: "退款超过客户实际付过的钱，最多能退 40 元" })
    )
    const { onDone } = setup()
    fireEvent.click(await screen.findByText("SL-20261010-0001"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText("退款超过客户实际付过的钱，最多能退 40 元")).toBeInTheDocument()
    expect(onDone).not.toHaveBeenCalled()
  })

  it("offers 补记退款 when the goods went back but the refund was not booked", async () => {
    vi.mocked(returnSaleBill)
      .mockResolvedValueOnce({
        bill_id: "r-1",
        bill_no: "RT-1",
        return_amount: "40",
        refund_error: "货已退回入库，但退款没有记上",
      })
      .mockResolvedValueOnce({ bill_id: "", bill_no: "", return_amount: "", refunded_amount: "40" })
    const { onDone, onOpenChange } = setup()
    fireEvent.click(await screen.findByText("SL-20261010-0001"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    fireEvent.click(screen.getByText("确认退货"))

    fireEvent.click(await screen.findByText("补记退款"))
    await waitFor(() => expect(returnSaleBill).toHaveBeenCalledTimes(2))
    expect(returnSaleBill).toHaveBeenLastCalledWith(
      "s-1",
      expect.objectContaining({ items: [], refund_only: true, refund_amount: "40.00", refund_method: "cash", return_bill_id: "r-1" }),
      "t1",
      expect.any(String)
    )
    await waitFor(() => expect(onDone).toHaveBeenCalledWith(expect.stringContaining("已补记退款")))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it("points to the receipt detail page when the refund prompt is dismissed", async () => {
    vi.mocked(returnSaleBill).mockResolvedValueOnce({
      bill_id: "r-1",
      bill_no: "RT-1",
      return_amount: "40",
      refund_error: "货已退回入库，但退款没有记上",
    })
    const { onDone } = setup()
    fireEvent.click(await screen.findByText("SL-20261010-0001"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    fireEvent.click(screen.getByText("确认退货"))
    fireEvent.click(await screen.findByText("稍后再说"))
    expect(onDone).toHaveBeenCalledWith(expect.stringContaining("可以到这张小票详情页点「补记退款」，或请老板、店长补记"))
  })

  it("keeps 补记退款 on screen with the reason when booking it fails", async () => {
    vi.mocked(returnSaleBill)
      .mockResolvedValueOnce({ bill_id: "r-1", bill_no: "RT-1", return_amount: "40", refund_error: "货已退回入库，但退款没有记上" })
      .mockRejectedValueOnce(new ApiError(500, "internal", "HTTP 500", { error: "internal", message: "服务繁忙，请稍后再试" }))
    const { onDone } = setup()
    fireEvent.click(await screen.findByText("SL-20261010-0001"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    fireEvent.click(screen.getByText("确认退货"))
    fireEvent.click(await screen.findByText("补记退款"))
    expect(await screen.findByText("服务繁忙，请稍后再试")).toBeInTheDocument()
    expect(screen.getByText("补记退款")).toBeInTheDocument()
    expect(onDone).not.toHaveBeenCalled()
  })

  it("says so when a receipt has nothing left to return", async () => {
    vi.mocked(getSaleBill).mockResolvedValue({
      ...(detail as object),
      items: [{ id: "line-1", product_id: "p-1", line_no: 1, qty: "5", unit_price: "20", line_amount: "100", returned_qty: "5" }],
    } as never)
    setup()
    fireEvent.click(await screen.findByText("SL-20261010-0001"))
    expect(await screen.findByText(/已经全部退完了/)).toBeInTheDocument()
  })

  it("shows a Chinese hint, not an error, when nothing matches", async () => {
    vi.mocked(listSaleBills).mockResolvedValue({ items: [], total: 0 })
    setup()
    expect(await screen.findByText("没有找到已出库的销售单")).toBeInTheDocument()
  })
})
