import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"

const { state } = vi.hoisted(() => ({ state: { role: "owner" } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner", tenantId: "t1" } } }),
}))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}))
vi.mock("@/lib/api/stock", () => ({
  listStockSnapshots: async () => [
    { id: "s1", product_id: "p1", warehouse_id: "w1", on_hand_qty: "5", available_qty: "5", unit_cost: "12.50", updated_at: "" },
  ],
}))
vi.mock("@/lib/api/products", () => ({
  listProducts: async () => ({ items: [{ id: "p1", name: "月季", code: "YJ" }] }),
}))
vi.mock("@/lib/api/warehouses", () => ({ listWarehouses: async () => ({ items: [{ id: "w1", name: "主仓" }] }) }))
vi.mock("@/components/stock-adjust-dialog", () => ({ StockAdjustDialog: () => null }))

import StockPage from "./page"
import { roleCan, type ShopRole } from "@/lib/roles"

describe("StockPage unit cost column", () => {
  beforeEach(() => {
    state.role = "owner"
  })

  it("shows the unit cost column to the owner", async () => {
    render(<StockPage />)
    expect(await screen.findByText("月季")).toBeInTheDocument()
    expect(screen.getByText("单位成本")).toBeInTheDocument()
  })

  it("hides the unit cost column from a clerk", async () => {
    state.role = "clerk"
    render(<StockPage />)
    expect(await screen.findByText("月季")).toBeInTheDocument()
    expect(screen.queryByText("单位成本")).toBeNull()
  })

  it("keeps count and export for a clerk, who may adjust and export stock", async () => {
    state.role = "clerk"
    render(<StockPage />)
    await screen.findByText("月季")
    expect(screen.getByText("盘点")).toBeInTheDocument()
    expect(screen.getByText("导出表格")).toBeInTheDocument()
  })

  it("hides count and export from a cashier", async () => {
    state.role = "cashier"
    render(<StockPage />)
    await screen.findByText("月季")
    expect(screen.queryByText("盘点")).toBeNull()
    expect(screen.queryByText("导出表格")).toBeNull()
  })

  it("hides count from finance, who may export but not adjust", async () => {
    state.role = "finance"
    render(<StockPage />)
    await screen.findByText("月季")
    expect(screen.queryByText("盘点")).toBeNull()
    expect(screen.getByText("导出表格")).toBeInTheDocument()
  })
})

describe("StockPage entries match the role's permissions", () => {
  it.each(["owner", "manager", "clerk", "cashier", "finance"] as const)("%s sees only the entries the backend accepts", async (role: ShopRole) => {
    state.role = role
    render(<StockPage />)
    await screen.findByText("月季")
    expect(!!screen.queryByText("盘点")).toBe(roleCan(role, "stock.adjust"))
    expect(!!screen.queryByText("导出表格")).toBe(roleCan(role, "export.stock"))
    expect(!!screen.queryByText("单位成本")).toBe(roleCan(role, "cost.view"))
  })
})
