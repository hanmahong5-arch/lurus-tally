import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

vi.mock("next/link", () => ({
  default: ({ href, children, ...props }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))
const replace = vi.fn()
vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard",
  useRouter: () => ({ push: replace, replace }),
}))
let profile = "hybrid"
vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: profile }),
  ProfileProvider: ({ children }: { children: React.ReactNode }) => children,
}))
vi.mock("@/components/account/account-card", () => ({ AccountCard: () => null }))
vi.mock("@/lib/api/todo", () => ({
  getTodoSummary: async () => ({ total: 0, ai_enabled: true }),
}))
let cashier = false
vi.mock("@/lib/api/cashier-mode", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/cashier-mode")>("@/lib/api/cashier-mode")
  return {
    ...actual,
    getCashierMode: async () => {
      actual.writeCashierFlag("t1", cashier)
      return { has_pin: true, tenant_id: "t1" }
    },
    setCashierPin: vi.fn(),
    verifyCashierPin: vi.fn(),
  }
})

import { DashboardSidebar } from "./sidebar"

beforeEach(() => {
  cashier = false
  profile = "hybrid"
  replace.mockReset()
})

describe("sidebar attention layers", () => {
  it("shows no 新 badge on any entry", async () => {
    const { container } = render(<DashboardSidebar />)
    await screen.findByText("收付款")
    await screen.findByText("AI 助手")
    expect(container.textContent).not.toContain("新")
  })
})

describe("sidebar in cashier mode", () => {
  beforeEach(() => {
    profile = "retail"
  })

  it("offers the PIN exit as a fallback", async () => {
    cashier = true
    render(<DashboardSidebar />)
    expect(await screen.findByRole("button", { name: "退出收银模式" })).toBeInTheDocument()
  })

  it("keeps the full menu and never redirects a shop that cannot use the POS", async () => {
    cashier = true
    profile = "horticulture"
    render(<DashboardSidebar />)
    expect(await screen.findByText("报表")).toBeInTheDocument()
    expect(replace).not.toHaveBeenCalled()
  })

  it("keeps only the POS and today's records", async () => {
    cashier = true
    render(<DashboardSidebar />)
    await waitFor(() => expect(screen.queryByRole("button", { name: "退出收银模式" })).not.toBeNull())
    expect(screen.getByText("POS 收银")).toBeInTheDocument()
    expect(screen.getByText("今日结单")).toBeInTheDocument()
    for (const hidden of ["报表", "收付款", "发票", "采购", "客户", "商品", "AI 助手"]) {
      expect(screen.queryByText(hidden)).toBeNull()
    }
  })

  it("sends the cashier off the dashboard to the POS", async () => {
    cashier = true
    render(<DashboardSidebar />)
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/pos"))
  })

  it("keeps the full menu when the mode is off", async () => {
    render(<DashboardSidebar />)
    expect(await screen.findByText("报表")).toBeInTheDocument()
    expect(replace).not.toHaveBeenCalled()
  })
})
