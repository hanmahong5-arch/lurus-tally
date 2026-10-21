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

describe("sidebar print settings entry", () => {
  beforeEach(() => {
    profile = "retail"
    summary = { total: 0 }
    calls = 0
  })

  it("offers 打印设置 in the settings group", async () => {
    render(<DashboardSidebar />)
    const link = await screen.findByRole("link", { name: /打印设置/ })
    expect(link.getAttribute("href")).toBe("/settings/print")
    await waitFor(() => expect(calls).toBeGreaterThan(0))
  })
})
