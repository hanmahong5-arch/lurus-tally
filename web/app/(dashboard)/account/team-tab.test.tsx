import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

import { ApiError } from "@/lib/api/errors"

const { api, confirmFn, state } = vi.hoisted(() => ({
  api: {
    listMembers: vi.fn(),
    listInvites: vi.fn(),
    updateMemberRole: vi.fn(),
    removeMember: vi.fn(),
    createInvite: vi.fn(),
    revokeInvite: vi.fn(),
  },
  confirmFn: vi.fn(),
  state: { viewerRole: "owner" as string | null },
}))
vi.mock("@/lib/api/team", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/team")>("@/lib/api/team")
  return { ...actual, ...api }
})
vi.mock("@/hooks/use-role", async () => {
  const actual = await vi.importActual<typeof import("@/hooks/use-role")>("@/hooks/use-role")
  return {
    ...actual,
    useRole: () => ({ role: state.viewerRole, known: state.viewerRole !== null, can: (p: string) => actual.roleCan(state.viewerRole as never, p as never) }),
  }
})
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => confirmFn }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { TeamTab } from "./team-tab"

const members = [
  { account_id: 1, role: "owner", role_label: "老板", display_name: "王老板", email_masked: "w***@x.com", phone_masked: "", joined_at: "2026-10-01T00:00:00Z", is_self: true },
  { account_id: 2, role: "clerk", role_label: "店员", display_name: "小李", email_masked: "l***@x.com", phone_masked: "138****0000", joined_at: "2026-10-02T00:00:00Z", is_self: false },
  { account_id: 3, role: "manager", role_label: "店长", display_name: "老张", email_masked: "z***@x.com", phone_masked: "", joined_at: "2026-10-03T00:00:00Z", is_self: false },
]

describe("TeamTab", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    state.viewerRole = "owner"
    api.listMembers.mockResolvedValue({ members, cashierAllowed: true })
    api.listInvites.mockResolvedValue([
      { id: "inv-1", role: "cashier", role_label: "收银", expires_at: "2026-10-20T00:00:00Z", created_at: "2026-10-10T00:00:00Z" },
    ])
  })

  it("lists members with Chinese role labels and no raw ids", async () => {
    const { container } = render(<TeamTab />)
    expect(await screen.findByText("小李")).toBeInTheDocument()
    expect(screen.getByText("王老板")).toBeInTheDocument()
    expect((screen.getByLabelText("小李的角色") as HTMLSelectElement).value).toBe("clerk")
    expect(screen.getByText("老板")).toBeInTheDocument()
    expect(container.textContent).not.toMatch(/\b(owner|manager|clerk|cashier|finance)\b/)
  })

  it.each([
    ["owner", true, "邀请店长、店员、收银、财务加入你的店铺"],
    ["owner", false, "邀请店长、店员、财务加入你的店铺"],
    ["manager", true, "邀请店员、收银、财务加入你的店铺"],
    ["manager", false, "邀请店员、财务加入你的店铺"],
  ])("header for %s with cashierAllowed=%s", async (role, cashierAllowed, text) => {
    state.viewerRole = role
    api.listMembers.mockResolvedValue({ members, cashierAllowed })
    render(<TeamTab />)
    expect(await screen.findByText(new RegExp(text))).toBeInTheDocument()
    if (role === "manager") expect(screen.queryByText(/邀请店长/)).toBeNull()
  })

  it("changes a member role after confirmation", async () => {
    confirmFn.mockResolvedValue(true)
    api.updateMemberRole.mockResolvedValue({ account_id: 2, role: "finance", role_label: "财务" })
    render(<TeamTab />)
    const select = await screen.findByLabelText("小李的角色")
    fireEvent.change(select, { target: { value: "finance" } })
    await waitFor(() => expect(api.updateMemberRole).toHaveBeenCalledWith(2, "finance"))
  })

  it("leaves the role alone when the change is not confirmed", async () => {
    confirmFn.mockResolvedValue(false)
    render(<TeamTab />)
    const select = await screen.findByLabelText("小李的角色")
    fireEvent.change(select, { target: { value: "finance" } })
    await waitFor(() => expect(confirmFn).toHaveBeenCalled())
    expect(api.updateMemberRole).not.toHaveBeenCalled()
  })

  it("does not offer role or remove controls on the owner row", async () => {
    render(<TeamTab />)
    await screen.findByText("小李")
    expect(screen.queryByLabelText("王老板的角色")).toBeNull()
    expect(screen.queryByLabelText("移除王老板")).toBeNull()
  })

  it("keeps a manager from editing the owner and other managers", async () => {
    state.viewerRole = "manager"
    render(<TeamTab />)
    await screen.findByText("小李")
    expect(screen.queryByLabelText("老张的角色")).toBeNull()
    expect(screen.getByLabelText("小李的角色")).toBeInTheDocument()
  })

  it("removes a member after confirmation", async () => {
    confirmFn.mockResolvedValue(true)
    api.removeMember.mockResolvedValue(undefined)
    render(<TeamTab />)
    fireEvent.click(await screen.findByLabelText("移除小李"))
    await waitFor(() => expect(api.removeMember).toHaveBeenCalledWith(2))
  })

  it("does not remove when the confirmation is declined", async () => {
    confirmFn.mockResolvedValue(false)
    render(<TeamTab />)
    fireEvent.click(await screen.findByLabelText("移除小李"))
    await waitFor(() => expect(confirmFn).toHaveBeenCalled())
    expect(api.removeMember).not.toHaveBeenCalled()
  })

  it("creates an invite and shows the full link with a QR code", async () => {
    api.createInvite.mockResolvedValue({
      invite_id: "inv-2", link: "/join?token=abc123", role: "clerk", role_label: "店员", expires_at: "2026-10-13T00:00:00Z",
    })
    render(<TeamTab />)
    await screen.findByText("小李")
    fireEvent.click(screen.getByRole("button", { name: "生成邀请链接" }))
    await waitFor(() => expect(api.createInvite).toHaveBeenCalledWith("clerk", 72))
    const input = (await screen.findByLabelText("邀请链接")) as HTMLInputElement
    expect(input.value).toBe(`${window.location.origin}/join?token=abc123`)
    expect(screen.getByRole("img", { name: "邀请二维码" })).toBeInTheDocument()
    expect(screen.getByText(/登录后点「加入店铺」/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "完成" }))
    expect(screen.queryByLabelText("邀请链接")).not.toBeInTheDocument()
  })

  it("revokes a pending invite", async () => {
    confirmFn.mockResolvedValue(true)
    api.revokeInvite.mockResolvedValue(undefined)
    render(<TeamTab />)
    fireEvent.click(await screen.findByLabelText("撤销收银邀请"))
    await waitFor(() => expect(api.revokeInvite).toHaveBeenCalledWith("inv-1"))
  })

  it("shows the server's Chinese message when listing fails", async () => {
    api.listMembers.mockRejectedValue(new ApiError(503, "team_unavailable", "团队功能暂时用不了，请稍后再试或联系客服"))
    render(<TeamTab />)
    expect(await screen.findByText(/团队功能暂时用不了/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "生成邀请链接" })).toBeNull()
    expect(screen.queryByText("待加入的邀请")).toBeNull()
  })

  it("shows a no-access note to roles that cannot manage the team", async () => {
    state.viewerRole = "clerk"
    render(<TeamTab />)
    expect(await screen.findByText(/只有老板或店长/)).toBeInTheDocument()
    expect(api.listMembers).not.toHaveBeenCalled()
  })

  it("shows no no-access note while the role is still unknown", async () => {
    state.viewerRole = null
    render(<TeamTab />)
    expect(screen.queryByText(/只有老板或店长/)).not.toBeInTheDocument()
    expect(api.listMembers).not.toHaveBeenCalled()
  })

  it("offers the cashier role in both dropdowns of a retail shop", async () => {
    render(<TeamTab />)
    const select = (await screen.findByLabelText("小李的角色")) as HTMLSelectElement
    expect(Array.from(select.options).map((o) => o.textContent)).toContain("收银")
    const invite = screen.getByLabelText("角色") as HTMLSelectElement
    expect(Array.from(invite.options).map((o) => o.textContent)).toContain("收银")
  })

  it("hides the cashier role in both dropdowns of a shop without a cashier desk", async () => {
    api.listMembers.mockResolvedValue({ members, cashierAllowed: false })
    render(<TeamTab />)
    const select = (await screen.findByLabelText("小李的角色")) as HTMLSelectElement
    expect(Array.from(select.options).map((o) => o.textContent)).not.toContain("收银")
    const invite = screen.getByLabelText("角色") as HTMLSelectElement
    expect(Array.from(invite.options).map((o) => o.textContent)).not.toContain("收银")
  })

  it("suggests switching an existing cashier to clerk in such a shop", async () => {
    api.listMembers.mockResolvedValue({
      members: [...members, { account_id: 4, role: "cashier", role_label: "收银", display_name: "小王", email_masked: "", phone_masked: "", joined_at: "2026-10-04T00:00:00Z", is_self: false, role_hint: "建议改为店员" }],
      cashierAllowed: false,
    })
    render(<TeamTab />)
    expect(await screen.findByText("建议改为店员")).toBeInTheDocument()
    const select = screen.getByLabelText("小王的角色") as HTMLSelectElement
    expect(select.value).toBe("cashier")
  })
})
