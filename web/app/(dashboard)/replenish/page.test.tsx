import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"

const { state } = vi.hoisted(() => ({ state: { role: "owner" } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner" } } }),
}))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
vi.mock("@/lib/api/replenish", () => ({
  listReplenishSuggestions: async () => ({
    items: [
      {
        product_id: "p1",
        product_name: "月季",
        product_code: "YJ",
        available_qty: "3",
        safety_qty: "10",
        avg_daily_sales: "1",
        suggested_qty: "20",
        est_amount_cny: "480.00",
        urgency_score: "2",
        lead_time_days: 3,
        in_transit: "0",
        rop: "12",
        safety_stock: "5",
        reason: "",
      },
    ],
    count: 1,
    weeks: 2,
  }),
  fetchScorecard: async () => {
    throw new Error("none")
  },
  draftBatch: vi.fn(),
}))

import ReplenishPage from "./page"

describe("ReplenishPage by role", () => {
  beforeEach(() => {
    state.role = "owner"
  })

  it("shows the estimated amount and the draft button to a manager", async () => {
    state.role = "manager"
    render(<ReplenishPage />)
    await screen.findByText("月季")
    expect(screen.getByText("预计金额")).toBeInTheDocument()
    expect(screen.getByText("¥480.00")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "生成采购草稿" })).toBeInTheDocument()
  })

  it("hides the estimated amount from a clerk but keeps drafting", async () => {
    state.role = "clerk"
    const { container } = render(<ReplenishPage />)
    await screen.findByText("月季")
    expect(screen.queryByText("预计金额")).toBeNull()
    expect(container.textContent).not.toContain("480")
    expect(screen.getByRole("button", { name: "生成采购草稿" })).toBeInTheDocument()
  })

  it("keeps finance read-only: amount shown, no drafting", async () => {
    state.role = "finance"
    render(<ReplenishPage />)
    await screen.findByText("月季")
    expect(screen.getByText("预计金额")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "生成采购草稿" })).toBeNull()
    expect(screen.queryByLabelText("全选")).toBeNull()
  })
})
