import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

const { state, loader } = vi.hoisted(() => ({
  state: { role: "owner", tab: "" },
  loader: vi.fn(),
}))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner" } } }),
}))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => new URLSearchParams(state.tab ? `tab=${state.tab}` : ""),
}))
vi.mock("next/link", () => ({ default: ({ children }: { children: React.ReactNode }) => <a>{children}</a> }))
vi.mock("next/image", () => ({ default: () => null }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => vi.fn() }))
vi.mock("@/components/account/account-drawer-provider", () => ({
  useAccountDrawer: () => ({ openDrawer: vi.fn() }),
}))
vi.mock("@/components/account/load-summary", () => ({ loadAccountSummary: loader }))
vi.mock("../subscription/plans-view", () => ({ SubscriptionPlansView: () => <div>套餐列表</div> }))
vi.mock("../settings/api-keys/page", () => ({ default: () => null }))
vi.mock("./team-tab", () => ({ TeamTab: () => null }))
vi.mock("@/lib/api/account", () => ({ fetchAccountSummary: vi.fn(), daysUntilExpiry: vi.fn() }))
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

const identity = (role: string, isOwner: boolean, profile = "cross_border") => ({
  identity: {
    user_id: "u1",
    tenant_id: "t1",
    email: "a@x.com",
    display_name: "小李",
    role,
    is_owner: isOwner,
    profile_type: profile,
  },
  billing: null,
  billingError: null,
})

const TABS = ["订阅 & 计费", "钱包流水", "接口密钥", "团队成员", "示例数据"]
function visible() {
  return TABS.filter((t) => screen.queryAllByText(t).length > 0)
}

describe("AccountCenter by shop role", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    state.role = "owner"
    state.tab = ""
    loader.mockResolvedValue(identity("owner", true))
  })

  it.each([
    ["owner", TABS],
    ["manager", ["团队成员", "示例数据"]],
    ["clerk", []],
    ["cashier", []],
    ["finance", []],
  ])("%s sees only the tabs the backend lets them use", async (role, expected) => {
    state.role = role
    render(<AccountCenter ssoConsoleURL="" />)
    await screen.findAllByText("个人资料")
    expect(visible()).toEqual(expected)
  })

  it("asks for billing only when the person may manage it", async () => {
    state.role = "clerk"
    loader.mockResolvedValue(identity("clerk", false))
    render(<AccountCenter ssoConsoleURL="" />)
    await waitFor(() => expect(loader).toHaveBeenCalledWith(false))
    expect(loader).not.toHaveBeenCalledWith(true)
  })

  it("names the role and the business type in Chinese", async () => {
    state.role = "manager"
    loader.mockResolvedValue(identity("admin", false, "cross_border"))
    const { container } = render(<AccountCenter ssoConsoleURL="" />)
    await screen.findByText("店长")
    expect(screen.getByText("跨境电商")).toBeInTheDocument()
    expect(container.textContent).not.toMatch(/\b(admin|cross_border)\b/)
  })

  it("tells a non-owner who opens the plan link why it is closed", async () => {
    state.role = "manager"
    state.tab = "subscription"
    render(<AccountCenter ssoConsoleURL="" />)
    expect(await screen.findByText("套餐和付费只有老板能看。")).toBeInTheDocument()
    expect(screen.queryByText("套餐列表")).toBeNull()
  })

  it("shows the owner the plan list on the same link", async () => {
    state.tab = "subscription"
    render(<AccountCenter ssoConsoleURL="" />)
    expect(await screen.findByText("套餐列表")).toBeInTheDocument()
  })
})
