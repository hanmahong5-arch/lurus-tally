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
let profile = "retail"
vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: profile }),
  ProfileProvider: ({ children }: { children: React.ReactNode }) => children,
}))
vi.mock("@/components/account/account-card", () => ({ AccountCard: () => null }))

let summary: Record<string, unknown> = { total: 0 }
let calls = 0
vi.mock("@/lib/api/todo", () => ({
  getTodoSummary: async () => {
    calls++
    return summary
  },
}))

import { DashboardSidebar } from "./sidebar"

describe("sidebar entries", () => {
  beforeEach(() => {
    profile = "retail"
    summary = { total: 0 }
    calls = 0
  })

  it("hides the AI assistant (and its badge) while the AI backend is not available", async () => {
    summary = { total: 0, ai_enabled: false }
    render(<DashboardSidebar />)
    await waitFor(() => expect(calls).toBeGreaterThan(0))
    expect(screen.queryByText("AI 助手")).toBeNull()
  })

  it("shows the AI assistant once the backend reports it is available", async () => {
    summary = { total: 0, ai_enabled: true }
    render(<DashboardSidebar />)
    expect(await screen.findByText("AI 助手")).toBeInTheDocument()
  })

  it("calls the payments entry 收付款 and uses no English section title or NEW tag", async () => {
    const { container } = render(<DashboardSidebar />)
    expect(await screen.findByText("收付款")).toBeInTheDocument()
    expect(screen.queryByText("付款")).toBeNull()
    await waitFor(() => expect(calls).toBeGreaterThan(0))
    expect(container.textContent).not.toMatch(/WORKSPACE|NEW/)
  })

  it("tucks Shopify and API keys under 高级 for a retail shop", async () => {
    render(<DashboardSidebar />)
    await screen.findByText(/^高级/)
    const group = screen.getByText(/^高级/).closest("details")!
    expect(group).not.toBeNull()
    expect(group.textContent).toContain("Shopify 店铺")
    expect(group.textContent).toContain("接口密钥")
  })

  it("keeps Shopify and API keys in the normal settings list for cross-border trade", () => {
    profile = "cross_border"
    render(<DashboardSidebar />)
    expect(screen.queryByText(/^高级/)).toBeNull()
    expect(screen.getByText("Shopify 店铺")).toBeInTheDocument()
  })
})
