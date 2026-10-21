import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "owner", isOwner: true } } }),
}))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => new URLSearchParams("tab=wallet"),
}))
vi.mock("next/link", () => ({ default: ({ children }: { children: React.ReactNode }) => <a>{children}</a> }))
vi.mock("next/image", () => ({ default: () => null }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => vi.fn() }))
vi.mock("@/components/account/account-drawer-provider", () => ({
  useAccountDrawer: () => ({ openDrawer: vi.fn() }),
}))
vi.mock("@/components/account/load-summary", () => ({
  loadAccountSummary: async () => ({
    identity: { user_id: "u1", tenant_id: "t1", email: "a@x.com", display_name: "小李", role: "owner", is_owner: true, profile_type: "retail" },
    billing: null,
    billingError: null,
  }),
}))
vi.mock("../subscription/plans-view", () => ({ SubscriptionPlansView: () => null }))
vi.mock("../settings/api-keys/page", () => ({ default: () => null }))
vi.mock("./team-tab", () => ({ TeamTab: () => null }))
vi.mock("@/lib/api/account", () => ({
  fetchAccountSummary: async () => ({
    identity: {},
    billing: {
      wallet: { available: 1, frozen: 0, total: 1 },
      subscription: { plan_code: "pro", status: "active", expires_at: null },
    },
    billingError: null,
  }),
  daysUntilExpiry: () => null,
}))
vi.mock("@/lib/api/account-extra", () => ({}))
vi.mock("@/lib/api/onboarding", () => ({ clearDemo: vi.fn() }))
vi.mock("@/lib/api/todo", () => ({ getTodoSummary: async () => ({ onboarding: { has_demo: false } }) }))

import { AccountCenter } from "./account-center"

describe("account center wallet tab in the trial period", () => {
  it("has no 去订阅页 link", async () => {
    const { container } = render(<AccountCenter ssoConsoleURL="x" />)
    await screen.findByText("专业版")
    expect(container.textContent).not.toContain("去订阅页")
  })
})
