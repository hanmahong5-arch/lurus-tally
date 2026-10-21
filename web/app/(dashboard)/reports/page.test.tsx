import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("@/auth", () => ({ auth: vi.fn().mockResolvedValue({ accessToken: "tok" }) }))
vi.mock("./analytics-blocks", () => ({
  GrossMarginBlock: () => null,
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

describe("ReportsPage month-end reconciliation", () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  it("asks the backend for the chosen month and shows the net figures", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => MONTHLY })
    render(await ReportsPage({ searchParams: { month: "2026-09" } }))
    expect(String(fetchMock.mock.calls[0][0])).toContain("/api/v1/reports/monthly?month=2026-09")
    expect(screen.getByText("¥900.00")).toBeTruthy()
    expect(screen.getByText("¥400.00")).toBeTruthy()
    expect(screen.getByText("¥1,234.50")).toBeTruthy()
  })

  it("links the downloads to the month's date range", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => MONTHLY })
    render(await ReportsPage({ searchParams: { month: "2026-09" } }))
    const hrefs = screen.getAllByRole("link").map((a) => a.getAttribute("href"))
    expect(hrefs).toContain("/api/proxy/exports/bills.csv?date_from=2026-09-01&date_to=2026-09-30&status=2")
    expect(hrefs).toContain("/api/proxy/exports/payments.csv?date_from=2026-09-01&date_to=2026-09-30")
  })

  it("points each card at the right place", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => MONTHLY })
    render(await ReportsPage({ searchParams: { month: "2026-09" } }))
    const hrefs = screen.getAllByRole("link").map((a) => a.getAttribute("href"))
    expect(hrefs).toContain("/api/proxy/exports/payments.csv?date_from=2026-09-01&date_to=2026-09-30&direction=in&counted_only=1")
    expect(hrefs).toContain("/api/proxy/exports/payments.csv?date_from=2026-09-01&date_to=2026-09-30&direction=out&counted_only=1")
    expect(hrefs).toContain("/payments?tab=sales&as_of=2026-09-30")
    expect(hrefs).toContain("/payments?tab=purchases&as_of=2026-09-30")
    // 销售成本 has no detail file, so only the cards that have one offer a download
    expect(screen.getAllByText("下载明细").length).toBe(6)
  })

  it("caps the aging as-of date at today while the month is still running", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: new Date("2026-09-15T04:00:00Z") })
    try {
      fetchMock.mockResolvedValue({ ok: true, json: async () => MONTHLY })
      render(await ReportsPage({ searchParams: { month: "2026-09" } }))
      const hrefs = screen.getAllByRole("link").map((a) => a.getAttribute("href"))
      expect(hrefs).toContain("/payments?tab=sales&as_of=2026-09-15")
      expect(hrefs).toContain("/payments?tab=purchases&as_of=2026-09-15")
    } finally {
      vi.useRealTimers()
    }
  })

  it("falls back to the current month for a junk month value", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => MONTHLY })
    render(await ReportsPage({ searchParams: { month: "abc" } }))
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/month=\d{4}-\d{2}$/)
  })

  it("shows a failure notice instead of zero when the request fails", async () => {
    fetchMock.mockResolvedValue({ ok: false, json: async () => ({}) })
    render(await ReportsPage({}))
    expect(screen.getAllByText("数据加载失败").length).toBe(9)
    expect(screen.queryByText("¥0.00")).toBeNull()
  })

  it("keeps internal wording off the page", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => MONTHLY })
    const { container } = render(await ReportsPage({}))
    expect(container.textContent).not.toMatch(/status=|ABC|AI 分析/)
  })
})
