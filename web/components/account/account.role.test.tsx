import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

const { state, loader, signOutMock } = vi.hoisted(() => ({ state: { role: "owner" }, loader: vi.fn(), signOutMock: vi.fn() }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner" } } }),
  signOut: (...a: unknown[]) => signOutMock(...a),
}))
vi.mock("next/image", () => ({ default: () => null }))
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("./load-summary", () => ({ loadAccountSummary: loader }))
vi.mock("./account-drawer-provider", () => ({
  useAccountDrawer: () => ({ open: true, openDrawer: vi.fn(), closeDrawer: vi.fn() }),
}))

import { AccountCard } from "./account-card"
import { AccountDrawer } from "./account-drawer"

function summary(withBilling: boolean) {
  return {
    identity: { user_id: "u", tenant_id: "12345678-aaaa-bbbb-cccc-000000000000", email: "a@x.com", display_name: "小李", role: "clerk", is_owner: false, profile_type: "retail" },
    billing: withBilling
      ? { subscription: { plan_code: "free", status: "active" }, wallet: { available: 10, frozen: 0, total: 10 }, entitlements: {} }
      : null,
    billingError: null,
  }
}

describe("account card and drawer by shop role", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    state.role = "owner"
  })

  it("shows the owner's card with the role name and asks for billing", async () => {
    loader.mockResolvedValue(summary(true))
    render(<AccountCard />)
    await screen.findByText("小李")
    expect(screen.getByText("老板")).toBeInTheDocument()
    expect(screen.queryByText("free")).toBeNull()
    expect(loader).toHaveBeenCalledWith(true)
  })

  it.each([
    ["manager", "店长"],
    ["clerk", "店员"],
    ["cashier", "收银"],
    ["finance", "财务"],
  ])("shows a %s the role name, never the plan, and never asks for billing", async (role, label) => {
    state.role = role
    loader.mockResolvedValue(summary(false))
    const { container } = render(<AccountCard />)
    await screen.findByText("小李")
    expect(screen.getByText(label)).toBeInTheDocument()
    expect(container.textContent).not.toMatch(/free|FREE|正常|需关注/)
    expect(loader).toHaveBeenCalledWith(false)
    expect(loader).not.toHaveBeenCalledWith(true)
  })

  it("shows the owner's drawer the plan and wallet, with no quota card for limits the system does not enforce", async () => {
    loader.mockResolvedValue(summary(true))
    render(<AccountDrawer />)
    expect(await screen.findByText("当前套餐")).toBeInTheDocument()
    expect(screen.getByText("钱包余额")).toBeInTheDocument()
    expect(screen.queryByText("配额")).toBeNull()
  })

  it("does not show a status next to the free plan when there is no subscription", async () => {
    loader.mockResolvedValue({ ...summary(true), billing: { subscription: null, wallet: { available: 10, frozen: 0, total: 10 }, entitlements: {} } })
    render(<AccountDrawer />)
    await screen.findByText("当前套餐")
    expect(screen.queryByText("未开通")).toBeNull()
  })

  it("shows a clerk's drawer only who they are", async () => {
    state.role = "clerk"
    loader.mockResolvedValue(summary(false))
    const { container } = render(<AccountDrawer />)
    await screen.findByText("小李")
    await waitFor(() => expect(screen.getByText("店员")).toBeInTheDocument())
    for (const t of ["当前套餐", "钱包余额", "配额", "升级", "充值"]) expect(screen.queryByText(new RegExp(t))).toBeNull()
    expect(container.textContent).not.toMatch(/12345678|free/)
    expect(loader).toHaveBeenCalledWith(false)
  })

  it("signs out from the drawer into a sign-in that asks for the account again", async () => {
    loader.mockResolvedValue(summary(true))
    render(<AccountDrawer />)
    fireEvent.click(await screen.findByRole("button", { name: "退出登录" }))
    await waitFor(() => expect(signOutMock).toHaveBeenCalledWith({ redirect: false, redirectTo: "/login?switch=1" }))
  })
})
