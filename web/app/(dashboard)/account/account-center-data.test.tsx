import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => new URLSearchParams("tab=data"),
}))
vi.mock("next/link", () => ({ default: ({ children }: { children: React.ReactNode }) => <a>{children}</a> }))
vi.mock("next/image", () => ({ default: () => null }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "owner", isOwner: true } }, status: "authenticated" }),
}))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => vi.fn() }))
vi.mock("@/components/account/account-drawer-provider", () => ({
  useAccountDrawer: () => ({ openDrawer: vi.fn() }),
}))
vi.mock("../subscription/plans-view", () => ({ SubscriptionPlansView: () => null }))
vi.mock("../settings/api-keys/page", () => ({ default: () => null }))
vi.mock("@/lib/api/account", () => ({ fetchAccountSummary: vi.fn(), daysUntilExpiry: vi.fn() }))
vi.mock("@/lib/api/account-extra", () => ({}))
vi.mock("@/lib/api/onboarding", () => ({ clearDemo: vi.fn() }))

let summary: unknown
vi.mock("@/lib/api/todo", () => ({
  getTodoSummary: async () => {
    if (summary instanceof Error) throw summary
    return summary
  },
}))

import { AccountCenter } from "./account-center"

describe("account center — 示例数据 tab", () => {
  beforeEach(() => {
    summary = { onboarding: { has_demo: true } }
  })

  it("offers the clear button while sample data exists", async () => {
    render(<AccountCenter ssoConsoleURL="" />)
    expect(await screen.findByRole("button", { name: "清除示例数据" })).toBeInTheDocument()
  })

  it("says there is nothing to clear otherwise", async () => {
    summary = { onboarding: { has_demo: false } }
    render(<AccountCenter ssoConsoleURL="" />)
    expect(await screen.findByText(/没有示例数据/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "清除示例数据" })).toBeNull()
  })

  it("does not claim there is no sample data when the read fails, and can retry", async () => {
    summary = new Error("boom")
    render(<AccountCenter ssoConsoleURL="" />)
    expect(await screen.findByText(/暂时查不到/)).toBeInTheDocument()
    expect(screen.queryByText(/没有示例数据/)).toBeNull()
    summary = { onboarding: { has_demo: true }, failed: [] }
    fireEvent.click(screen.getByRole("button", { name: "重试" }))
    expect(await screen.findByRole("button", { name: "清除示例数据" })).toBeInTheDocument()
  })

  it("treats a failed onboarding source as unknown, not as empty", async () => {
    summary = { onboarding: { has_demo: false }, failed: ["onboarding"] }
    render(<AccountCenter ssoConsoleURL="" />)
    expect(await screen.findByText(/暂时查不到/)).toBeInTheDocument()
  })
})
