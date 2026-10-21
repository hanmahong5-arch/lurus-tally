import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

vi.mock("next/link", () => ({
  default: ({ href, children, ...props }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))
vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard",
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: "retail" }),
  ProfileProvider: ({ children }: { children: React.ReactNode }) => children,
}))
vi.mock("@/components/account/account-card", () => ({ AccountCard: () => null }))
const { state } = vi.hoisted(() => ({ state: { role: "owner" as string | null } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner" } } }),
}))
let calls = 0
vi.mock("@/lib/api/todo", () => ({
  getTodoSummary: async () => {
    calls++
    return { total: 0, ai_enabled: true }
  },
}))

import { DashboardSidebar } from "./sidebar"

async function renderSidebar() {
  render(<DashboardSidebar />)
  await waitFor(() => expect(calls).toBeGreaterThan(0))
  await screen.findByText("商品")
}

describe("sidebar by shop role", () => {
  beforeEach(() => {
    calls = 0
    state.role = "owner"
  })

  it("shows billing and the team page to the owner", async () => {
    await renderSidebar()
    expect(screen.getByText("订阅")).toBeInTheDocument()
    expect(screen.getByText("团队成员")).toBeInTheDocument()
    expect(screen.getByText("报表")).toBeInTheDocument()
  })

  it("hides billing from a manager but keeps the team page", async () => {
    state.role = "manager"
    await renderSidebar()
    expect(screen.queryByText("订阅")).toBeNull()
    expect(screen.getByText("团队成员")).toBeInTheDocument()
  })

  it("hides billing, team, reports and payments from a clerk", async () => {
    state.role = "clerk"
    await renderSidebar()
    for (const label of ["订阅", "团队成员", "报表", "收付款", "发票", "打印设置"]) {
      expect(screen.queryByText(label)).toBeNull()
    }
    expect(screen.getByText("商品")).toBeInTheDocument()
  })

  it("keeps reports and payments for finance but not billing or team", async () => {
    state.role = "finance"
    await renderSidebar()
    expect(screen.getByText("报表")).toBeInTheDocument()
    expect(screen.getByText("收付款")).toBeInTheDocument()
    expect(screen.queryByText("订阅")).toBeNull()
    expect(screen.queryByText("团队成员")).toBeNull()
  })

  it("keeps purchases for a clerk, who can start drafts", async () => {
    state.role = "clerk"
    await renderSidebar()
    expect(screen.getByText("采购")).toBeInTheDocument()
  })

  it("hides purchases from a cashier", async () => {
    state.role = "cashier"
    await renderSidebar()
    expect(screen.queryByText("采购")).toBeNull()
  })

  it("shows purchases to finance", async () => {
    state.role = "finance"
    await renderSidebar()
    expect(screen.getByText("采购")).toBeInTheDocument()
  })

  it.each([
    ["owner", true],
    ["manager", true],
    ["cashier", true],
    ["clerk", false],
    ["finance", false],
  ])("POS entry for %s: %s", async (role, shown) => {
    state.role = role
    await renderSidebar()
    if (shown) expect(screen.getByText("POS 收银")).toBeInTheDocument()
    else expect(screen.queryByText("POS 收银")).toBeNull()
  })
})
