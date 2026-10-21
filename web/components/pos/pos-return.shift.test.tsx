import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"

const { state } = vi.hoisted(() => ({ state: { role: "cashier" as string } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner" } } }),
}))
vi.mock("@/lib/api/sale", () => ({
  listSaleBills: vi.fn(),
  getSaleBill: vi.fn(),
  returnSaleBill: vi.fn(),
}))
vi.mock("@/hooks/use-product-names", () => ({ useProductNames: () => ({ "p-1": "螺丝" }) }))

import { listSaleBills, getSaleBill, returnSaleBill } from "@/lib/api/sale"
import { PosReturn } from "./pos-return"

const head = { id: "s-1", bill_no: "SL-1", status: 2, bill_date: "2026-10-10T08:00:00Z", total_amount: "100", paid_amount: "100" }
const detail = {
  head,
  items: [{ id: "line-1", product_id: "p-1", line_no: 1, qty: "5", unit_price: "20", line_amount: "100", unit_name: "个" }],
  payments: [{ id: "pay-1", pay_type: "cash", amount: "100" }],
  returned_amount: "0",
} as never

async function leaveWithoutRefund(role: string) {
  state.role = role
  vi.mocked(listSaleBills).mockResolvedValue({ items: [head], total: 1 } as never)
  vi.mocked(getSaleBill).mockResolvedValue(detail)
  vi.mocked(returnSaleBill).mockResolvedValue({ bill_id: "r-1", bill_no: "RT-1", return_amount: "40", refund_error: "没记上" } as never)
  const onDone = vi.fn()
  render(<PosReturn open onOpenChange={vi.fn()} tenantId="t1" onDone={onDone} />)
  fireEvent.click(await screen.findByText("SL-1"))
  fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
  fireEvent.click(await screen.findByLabelText(/同时退款/))
  fireEvent.click(screen.getByText("确认退货"))
  return onDone
}

describe("PosReturn missed refund by shop role", () => {
  beforeEach(() => vi.clearAllMocks())

  it("tells a cashier to ask the owner or manager, not to open the bill page", async () => {
    const onDone = await leaveWithoutRefund("cashier")
    await screen.findByText("补记退款")
    fireEvent.click(screen.getByText("稍后再说"))
    expect(onDone).toHaveBeenCalledWith(expect.stringContaining("或请老板、店长补记"))
    expect(onDone).not.toHaveBeenCalledWith(expect.stringContaining("详情页"))
  })

  it("keeps the detail-page hint for a manager", async () => {
    const onDone = await leaveWithoutRefund("manager")
    await screen.findByText("补记退款")
    fireEvent.click(screen.getByText("稍后再说"))
    expect(onDone).toHaveBeenCalledWith(expect.stringContaining("详情页"))
  })
})
