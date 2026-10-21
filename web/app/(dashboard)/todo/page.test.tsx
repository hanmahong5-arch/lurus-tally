import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("@/auth", () => ({ auth: vi.fn().mockResolvedValue({ accessToken: "tok" }) }))
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))

import TodoPage from "./page"

const empty = { count: 0, items: [] }
const bill = (n: number) => ({
  id: `id-${n}`,
  bill_no: `SL-${n}`,
  partner_name: "老王",
  total_amount: "100.00",
  bill_date: "2026-10-10",
})

function summary(over: Record<string, unknown> = {}) {
  return {
    draft_purchases: empty,
    draft_sales: empty,
    low_stock: empty,
    overdue_customers: empty,
    uninvoiced_sales: empty,
    kpis: { today_sales: "0", today_collected: "0", receivable_unpaid: "0", month_uninvoiced: "0" },
    total: 0,
    has_data: true,
    failed: [],
    generated_at: "",
    ...over,
  }
}

describe("TodoPage", () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  it("shows the real draft count (6), not the preview size", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () =>
        summary({ draft_sales: { count: 6, items: [bill(1), bill(2), bill(3), bill(4), bill(5)] }, total: 6 }),
    })
    render(await TodoPage())
    expect(screen.getByText("6")).toBeInTheDocument()
    expect(screen.getByText("共 6 项需要你看一眼")).toBeInTheDocument()
    expect(screen.getAllByText("去审核").length).toBe(5)
  })

  it("never says 通关 when the backend fails", async () => {
    fetchMock.mockResolvedValue({ ok: false, json: async () => ({}) })
    const { container } = render(await TodoPage())
    expect(screen.getByText("待办数据加载失败")).toBeInTheDocument()
    expect(container.textContent).not.toContain("通关")
    expect(container.textContent).not.toContain("一切都好")
  })

  it("flags partial failure instead of an all-clear", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => summary({ failed: ["low_stock"] }) })
    const { container } = render(await TodoPage())
    expect(screen.getAllByText("部分数据加载失败").length).toBeGreaterThan(0)
    expect(container.textContent).not.toContain("一切都好")
    expect(container.textContent).not.toContain("通关")
  })

  it("shows the all-clear only when every source answered and nothing is pending", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => summary() })
    render(await TodoPage())
    expect(screen.getByText("一切都好")).toBeInTheDocument()
  })

  it("renders the negative-stock group when present, and hides it when absent", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () =>
        summary({
          negative_stock: {
            count: 1,
            items: [{ product_id: "p1", product_name: "红富士", warehouse_name: "主仓", on_hand: "-3" }],
          },
          total: 1,
        }),
    })
    render(await TodoPage())
    expect(screen.getByText("离线销售导致负库存，请盘点")).toBeInTheDocument()
    expect(screen.getByText("红富士")).toBeInTheDocument()
    expect(screen.getByText("主仓")).toBeInTheDocument()
    expect(screen.getByText("-3")).toBeInTheDocument()
  })

  it("does not render the negative-stock group when undefined", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => summary({ draft_sales: { count: 1, items: [bill(1)] }, total: 1 }) })
    const { container } = render(await TodoPage())
    expect(container.textContent).not.toContain("负库存")
  })
})
