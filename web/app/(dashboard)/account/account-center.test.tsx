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

describe("AccountCenter export-all card", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    state.tab = ""
  })

  it.each(["owner", "manager", "finance"])("%s sees the card and a download link", async (role) => {
    state.role = role
    loader.mockResolvedValue(identity(role, role === "owner"))
    render(<AccountCenter ssoConsoleURL="" />)
    expect(await screen.findByText("导出全部数据")).toBeInTheDocument()
    expect(screen.getByText("包含商品、客户、供应商、库存、单据、收付款和发票，可用表格软件打开。")).toBeInTheDocument()
    expect(screen.getByText("下载全部数据").closest("a")).toHaveAttribute("href", "/api/proxy/exports/all.zip")
  })

  it.each(["clerk", "cashier"])("%s does not see the card", async (role) => {
    state.role = role
    loader.mockResolvedValue(identity(role, false))
    render(<AccountCenter ssoConsoleURL="" />)
    await screen.findAllByText("个人资料")
    await waitFor(() => expect(loader).toHaveBeenCalled())
    expect(screen.queryByText("导出全部数据")).toBeNull()
  })
})
