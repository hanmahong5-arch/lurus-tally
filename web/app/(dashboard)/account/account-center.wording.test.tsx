import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"

const { state } = vi.hoisted(() => ({ state: { tab: "" } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "owner", isOwner: true } } }),
}))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => new URLSearchParams(`tab=${state.tab}`),
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
    identity: { user_id: "u1", tenant_id: "t1", email: "a@x.com", display_name: "小李", role: "owner", is_owner: true, profile_type: "cross_border" },
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
vi.mock("@/lib/api/account-extra", () => ({
  getProfile: async () => ({ display_name: "小李", phone: "" }),
  updateProfile: vi.fn(),
  uploadAvatar: vi.fn(),
  listSessions: async () => ({ items: [] }),
  revokeSession: vi.fn(),
  listAuditLog: async () => ({ items: [], total: 0 }),
}))
vi.mock("@/lib/api/onboarding", () => ({ clearDemo: vi.fn() }))
vi.mock("@/lib/api/todo", () => ({ getTodoSummary: async () => ({ onboarding: { has_demo: false } }) }))

import { AccountCenter } from "./account-center"
import { planLabel, subscriptionStatusLabel } from "@/lib/plan-labels"

describe("account center wording", () => {
  it("profile tab calls the identity service 登录服务", async () => {
    state.tab = "profile"
    const { container } = render(<AccountCenter ssoConsoleURL="x" />)
    await screen.findByText(/由登录服务管理/)
    expect(container.textContent).not.toMatch(/SSO/)
  })

  it("wallet tab says 平台 and shows the plan and status in Chinese", async () => {
    state.tab = "wallet"
    const { container } = render(<AccountCenter ssoConsoleURL="x" />)
    await screen.findByText("专业版")
    expect(container.textContent).toContain("使用中")
    expect(container.textContent).toContain("由系统统一管理")
    expect(container.textContent).not.toMatch(/lurus-platform|流水 API|\bpro\b|\bactive\b/)
  })

  it("security tab has no SSO wording", async () => {
    state.tab = "security"
    const { container } = render(<AccountCenter ssoConsoleURL="x" />)
    await screen.findByText(/由登录服务统一管理/)
    expect(container.textContent).toContain("打开登录服务控制台")
    expect(container.textContent).not.toMatch(/SSO/)
  })
})

describe("plan-labels", () => {
  it("maps known codes and never leaks unknown ones", () => {
    expect(planLabel("free")).toBe("免费版")
    expect(planLabel("pro_yearly")).toBe("专业版（年付）")
    expect(planLabel("enterprise_yearly")).not.toBe("当前套餐")
    expect(planLabel("weird_code")).toBe("其他套餐")
    expect(subscriptionStatusLabel("expired")).toBe("已到期")
    expect(subscriptionStatusLabel("zzz")).toBe("状态未知")
    expect(subscriptionStatusLabel(undefined)).toBe("未开通")
  })
})
