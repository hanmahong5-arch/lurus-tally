import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen } from "@testing-library/react"

let profileType = "retail"
// Shop role of the signed-in person; null = the session does not carry one.
let shopRole: string | null = null
vi.mock("@/auth", () => ({
  auth: vi.fn(async () => ({
    accessToken: "tok",
    user: { profileType, role: shopRole, isOwner: shopRole === "owner" },
  })),
}))
vi.mock("next/navigation", async () => {
  const actual = await vi.importActual<typeof import("next/navigation")>("next/navigation")
  return {
    ...actual,
    redirect: (to: string) => {
      throw new Error(`REDIRECT:${to}`)
    },
  }
})
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("@/components/dashboard/monday-card", () => ({ MondayCard: () => null }))
vi.mock("@/components/onboarding/ClearDemoCard", () => ({ ClearDemoCard: () => <button>清除示例数据</button> }))

import DashboardPage from "./page"
import { ProfileProvider } from "@/lib/profile"

const empty = { count: 0, items: [] }
function summary(over: Record<string, unknown> = {}) {
  return {
    draft_purchases: empty,
    draft_sales: empty,
    low_stock: empty,
    overdue_customers: empty,
    uninvoiced_sales: empty,
    kpis: { today_sales: "1381.00", today_collected: "261.00", receivable_unpaid: "9586.30", month_uninvoiced: "7720.80" },
    total: 0,
    has_data: true,
    onboarding: { has_product: true, has_purchase: true, has_sale: true, has_payment: true, has_demo: false },
    ai_enabled: false,
    failed: [],
    generated_at: "",
    ...over,
  }
}

describe("DashboardPage", () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    shopRole = null
    fetchMock.mockReset()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.clear()
  })

  function mockBackend(todo: unknown) {
    fetchMock.mockImplementation(async (url: string) => {
      if (String(url).includes("/cashier-mode")) return { ok: true, status: 200, json: async () => ({ has_pin: true, tenant_id: "t1" }) }
      if (String(url).includes("/todo/summary")) {
        return todo === null ? { ok: false, json: async () => ({}) } : { ok: true, json: async () => todo }
      }
      return { ok: false, json: async () => ({}) }
    })
  }

  it("shows the four KPIs and drops the entry-card wall", async () => {
    mockBackend(summary({ draft_sales: { count: 6, items: [] }, total: 6 }))
    render(await DashboardPage({}))
    for (const l of ["今日销售", "今日回款", "应收未收", "本月未开票"]) expect(screen.getByText(l)).toBeInTheDocument()
    expect(screen.getByText("¥1,381.00")).toBeInTheDocument()
    expect(screen.queryByText("商品管理")).toBeNull()
    expect(screen.queryByText("采购管理")).toBeNull()
    expect(screen.getByText("待你处理")).toBeInTheDocument()
    expect(screen.getByText("张销售单等你审核")).toBeInTheDocument()
    expect(screen.queryByText("新手三步，开始记第一笔账")).toBeNull()
  })

  const none = { has_product: false, has_purchase: false, has_sale: false, has_payment: false, has_demo: false }

  it("shows all three getting-started steps for a brand-new tenant", async () => {
    mockBackend(summary({ has_data: false, onboarding: none }))
    render(await DashboardPage({}))
    expect(screen.getByText(/新手三步，开始记第一笔账/)).toBeInTheDocument()
    expect(screen.getByText(/0\/3/)).toBeInTheDocument()
    expect(screen.getAllByRole("link", { name: /去添加|去导入|去开单/ })).toHaveLength(3)
  })

  it("keeps the checklist with progress after the first product is added", async () => {
    mockBackend(summary({ onboarding: { ...none, has_product: true } }))
    render(await DashboardPage({}))
    expect(screen.getByText(/1\/3/)).toBeInTheDocument()
    expect(screen.getByText("已完成")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "去导入" })).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "去添加" })).toBeNull()
  })

  it("sample data does not tick a step and offers to clear it", async () => {
    mockBackend(summary({ onboarding: { ...none, has_demo: true } }))
    render(await DashboardPage({}))
    expect(screen.getByText(/0\/3/)).toBeInTheDocument()
    expect(screen.getByText("清除示例数据")).toBeInTheDocument()
  })

  it("hides the clear-sample-data button in cashier mode", async () => {
    profileType = "retail"
    window.localStorage.setItem("tally:cashier-mode:t1", "1")
    fetchMock.mockImplementation(async (url: string) => {
      if (String(url).includes("/cashier-mode")) return { ok: true, status: 200, json: async () => ({ has_pin: true, tenant_id: "t1" }) }
      if (String(url).includes("/todo/summary")) return { ok: true, json: async () => summary({ onboarding: { ...none, has_demo: true } }) }
      return { ok: false, json: async () => ({}) }
    })
    render(<ProfileProvider value={{ profileType: "retail" }}>{await DashboardPage({})}</ProfileProvider>)
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.queryByText("清除示例数据")).toBeNull()
  })

  it("hides the checklist only when all three steps are done", async () => {
    mockBackend(summary({ onboarding: { ...none, has_product: true, has_purchase: true, has_sale: true } }))
    render(await DashboardPage({}))
    expect(screen.queryByText(/新手三步/)).toBeNull()
    expect(screen.queryByText("清除示例数据")).toBeNull()
  })

  it("keeps the checklist and banner when an unrelated source failed", async () => {
    mockBackend(summary({ failed: ["low_stock"], onboarding: { ...none, has_demo: true } }))
    render(await DashboardPage({}))
    expect(screen.getByText(/新手三步/)).toBeInTheDocument()
    expect(screen.getByText("清除示例数据")).toBeInTheDocument()
  })

  it("hides the checklist and banner when the onboarding source itself failed", async () => {
    mockBackend(summary({ failed: ["onboarding"], onboarding: { ...none, has_demo: true } }))
    render(await DashboardPage({}))
    expect(screen.queryByText(/新手三步/)).toBeNull()
    expect(screen.queryByText("清除示例数据")).toBeNull()
  })

  it("step 2 is about importing goods and opening stock, with the import as the main button", async () => {
    mockBackend(summary({ onboarding: none }))
    render(await DashboardPage({}))
    expect(screen.getByText("导入商品和期初库存（或记一笔进货）")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "去导入" })).toHaveAttribute("href", "/products?import=1")
  })

  it("offers a retail owner the cashier-mode switch", async () => {
    profileType = "retail"
    mockBackend(summary())
    render(await DashboardPage({}))
    expect(await screen.findByRole("button", { name: "开启收银模式" })).toBeInTheDocument()
  })

  it.each(["horticulture", "cross_border", "hybrid"])("offers no cashier-mode switch to a %s shop", async (p) => {
    profileType = p
    mockBackend(summary())
    render(await DashboardPage({}))
    await screen.findByText("今日销售")
    expect(screen.queryByRole("button", { name: "开启收银模式" })).toBeNull()
  })

  it("mentions the AI shortcut only when the assistant is available", async () => {
    mockBackend(summary({ ai_enabled: false }))
    const { container, unmount } = render(await DashboardPage({}))
    expect(container.textContent).not.toContain("AI 助手")
    unmount()
    mockBackend(summary({ ai_enabled: true }))
    const second = render(await DashboardPage({}))
    expect(second.container.textContent).toContain("AI 助手")
  })

  it("uses no costing jargon in the header", async () => {
    vi.mocked((await import("@/auth")).auth).mockResolvedValueOnce({ accessToken: "tok", user: { profileType: "retail" } } as never)
    mockBackend(summary())
    const { container } = render(await DashboardPage({}))
    expect(container.textContent).not.toMatch(/WAC|FIFO/)
  })

  it("shows a failure notice and dashes, not zeros, when the backend fails", async () => {
    mockBackend(null)
    const { container } = render(await DashboardPage({}))
    expect(screen.getByText("首页数据加载失败")).toBeInTheDocument()
    expect(container.textContent).not.toContain("¥0.00")
    expect(container.textContent).not.toContain("没有需要处理")
  })

  describe("by shop role", () => {
    const none = { has_product: false, has_purchase: false, has_sale: false, has_payment: false, has_demo: true }
    const busy = summary({
      draft_sales: { count: 2, items: [] },
      draft_purchases: { count: 1, items: [] },
      overdue_customers: { count: 3, items: [] },
      uninvoiced_sales: { count: 4, items: [] },
      low_stock: { count: 5, items: [] },
      onboarding: none,
      ai_enabled: true,
      total: 15,
    })

    it("sends a cashier of a retail shop to the till", async () => {
      shopRole = "cashier"
      profileType = "retail"
      mockBackend(busy)
      await expect(DashboardPage({})).rejects.toThrow("REDIRECT:/pos")
    })

    it("leaves a manager every entry", async () => {
      shopRole = "manager"
      profileType = "retail"
      mockBackend(busy)
      render(await DashboardPage({}))
      expect(screen.getByText("今日回款")).toBeInTheDocument()
      expect(screen.getAllByRole("link", { name: "去审核" })).toHaveLength(2)
      expect(screen.getByRole("link", { name: "去收款" })).toBeInTheDocument()
      expect(screen.getByRole("link", { name: "去开票" })).toBeInTheDocument()
      expect(screen.getByRole("link", { name: "去补货" })).toBeInTheDocument()
      expect(screen.getByText("清除示例数据")).toBeInTheDocument()
    })

    it("shows a clerk no approve, collect, invoice, money, setup or assistant entries", async () => {
      shopRole = "clerk"
      profileType = "retail"
      mockBackend(busy)
      const { container } = render(await DashboardPage({}))
      expect(screen.queryByRole("link", { name: "去审核" })).toBeNull()
      expect(screen.queryByRole("link", { name: "去收款" })).toBeNull()
      expect(screen.queryByRole("link", { name: "去开票" })).toBeNull()
      expect(screen.getByRole("link", { name: "去补货" })).toBeInTheDocument()
      for (const l of ["今日回款", "应收未收", "本月未开票"]) expect(screen.queryByText(l)).toBeNull()
      expect(screen.getByText("今日销售")).toBeInTheDocument()
      expect(screen.queryByText("清除示例数据")).toBeNull()
      expect(screen.queryByText(/新手三步/)).toBeNull()
      expect(screen.queryByRole("button", { name: "开启收银模式" })).toBeNull()
      expect(container.textContent).toContain("⌘J")
    })

    it("shows finance the money figures and the collect and invoice buttons only", async () => {
      shopRole = "finance"
      profileType = "retail"
      mockBackend(busy)
      render(await DashboardPage({}))
      expect(screen.getByText("应收未收")).toBeInTheDocument()
      expect(screen.getByRole("link", { name: "去收款" })).toBeInTheDocument()
      expect(screen.getByRole("link", { name: "去开票" })).toBeInTheDocument()
      expect(screen.queryByRole("link", { name: "去审核" })).toBeNull()
      expect(screen.queryByText("清除示例数据")).toBeNull()
    })
  })
})
