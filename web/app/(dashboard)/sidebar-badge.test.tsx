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
  useProfile: () => ({ profileType: "hybrid" }),
  ProfileProvider: ({ children }: { children: React.ReactNode }) => children,
}))
vi.mock("@/components/account/account-card", () => ({ AccountCard: () => null }))

// A plain function (not vi.fn) so a rejected result is not tracked as unhandled by the mock.
let todoImpl: () => Promise<unknown> = async () => ({ total: 0 })
let todoCalls = 0
vi.mock("@/lib/api/todo", () => ({
  getTodoSummary: () => {
    todoCalls++
    return todoImpl()
  },
}))

import { DashboardSidebar } from "./sidebar"

describe("sidebar todo badge", () => {
  beforeEach(() => {
    todoCalls = 0
  })

  it("shows the pending total next to 待办", async () => {
    todoImpl = async () => ({ total: 6 })
    render(<DashboardSidebar />)
    await waitFor(() => expect(screen.getByLabelText("6 项待办")).toBeInTheDocument())
  })

  it("shows no badge when nothing is pending or the read fails", async () => {
    todoImpl = () => Promise.reject(new Error("down"))
    render(<DashboardSidebar />)
    await waitFor(() => expect(todoCalls).toBeGreaterThan(0))
    expect(screen.queryByLabelText(/项待办/)).toBeNull()
  })

  it("caps the badge at 99+", async () => {
    todoImpl = async () => ({ total: 250 })
    render(<DashboardSidebar />)
    await waitFor(() => expect(screen.getByText("99+")).toBeInTheDocument())
  })
})
