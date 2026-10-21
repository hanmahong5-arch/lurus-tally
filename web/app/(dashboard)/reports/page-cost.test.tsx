import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen } from "@testing-library/react"

const { authMock } = vi.hoisted(() => ({ authMock: vi.fn() }))
vi.mock("@/auth", () => ({ auth: authMock }))
const grossMargin = vi.hoisted(() => vi.fn(() => null))
vi.mock("./analytics-blocks", () => ({
  GrossMarginBlock: grossMargin,
  ABCBlock: () => null,
  DeadStockBlock: () => null,
  SalesTopBlock: () => null,
}))

import ReportsPage from "./page"

const MONTHLY = {
  month: "2026-09",
  date_from: "2026-09-01",
  date_to: "2026-09-30",
  net_sales: "900.00",
  cost_of_goods: "500.00",
  gross_profit: "400.00",
  net_purchases: "750.00",
  receipts: "600.00",
  payments: "300.00",
  receivable: "42.00",
  payable: "58.00",
  inventory_value: "1234.50",
}

describe("ReportsPage cost visibility", () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    fetchMock.mockReset()
    grossMargin.mockClear()
    fetchMock.mockResolvedValue({ ok: true, json: async () => MONTHLY })
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  it("shows cost, profit and stock value to finance", async () => {
    authMock.mockResolvedValue({ accessToken: "tok", user: { role: "finance", isOwner: false } })
    render(await ReportsPage({ searchParams: { month: "2026-09" } }))
    expect(screen.getByText("销售成本")).toBeInTheDocument()
    expect(screen.getByText("毛利")).toBeInTheDocument()
    expect(screen.getByText("库存金额")).toBeInTheDocument()
    expect(grossMargin).toHaveBeenCalled()
  })

  it("gives a clerk only a notice: no report request, no figures", async () => {
    authMock.mockResolvedValue({ accessToken: "tok", user: { role: "clerk", isOwner: false } })
    render(await ReportsPage({ searchParams: { month: "2026-09" } }))
    expect(screen.getByText("报表只对老板、店长和财务开放")).toBeInTheDocument()
    expect(screen.queryByText("净销售额")).toBeNull()
    expect(screen.queryByText("销售成本")).toBeNull()
    expect(grossMargin).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
