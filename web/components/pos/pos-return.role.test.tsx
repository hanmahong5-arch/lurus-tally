import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"

const { state } = vi.hoisted(() => ({ state: { role: "cashier" as string } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner" } } }),
}))
vi.mock("@/lib/api/sale", () => ({
  listSaleBills: vi.fn(),
  getSaleBill: vi.fn(),
  returnSaleBill: vi.fn(),
}))
vi.mock("@/hooks/use-product-names", () => ({ useProductNames: () => ({}) }))

import { listSaleBills } from "@/lib/api/sale"
import { PosReturn } from "./pos-return"

function setup() {
  render(<PosReturn open onOpenChange={vi.fn()} tenantId="t1" onDone={vi.fn()} />)
}

describe("PosReturn by shop role", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(listSaleBills).mockResolvedValue({ items: [], total: 0 })
  })

  it("lists only today's own receipts for a cashier and says who handles the rest", async () => {
    state.role = "cashier"
    setup()
    await screen.findByText(/今天你还没有开过可以退的小票/)
    expect(listSaleBills).toHaveBeenCalledWith(
      expect.objectContaining({ mine: true, date_from: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/) }),
    )
    const arg = vi.mocked(listSaleBills).mock.calls[0][0]
    expect(arg?.date_to).toBe(arg?.date_from)
    expect(screen.getAllByText(/请找老板或店长/).length).toBeGreaterThan(0)
  })

  it("lists every approved sale for a manager", async () => {
    state.role = "manager"
    setup()
    await screen.findByText("没有找到已出库的销售单")
    const arg = vi.mocked(listSaleBills).mock.calls[0][0]
    expect(arg?.mine).toBeUndefined()
    expect(arg?.date_from).toBeUndefined()
  })

  it("does not look anything up for a role with no right to return", async () => {
    state.role = "clerk"
    setup()
    expect(await screen.findByText("退货请找老板或店长")).toBeInTheDocument()
    expect(listSaleBills).not.toHaveBeenCalled()
  })
})
