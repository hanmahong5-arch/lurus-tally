import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

import { ApiError } from "@/lib/api/errors"

const { joinShop, replace, update, search, confirmFn, session, signOutForSwitch, fetchMyShopName } = vi.hoisted(() => ({
  session: { value: null as null | Record<string, unknown> },
  signOutForSwitch: vi.fn(),
  fetchMyShopName: vi.fn(),
  joinShop: vi.fn(),
  confirmFn: vi.fn(),
  replace: vi.fn(),
  update: vi.fn(),
  search: { value: "token=abc" },
}))
vi.mock("@/lib/api/team", () => ({ joinShop }))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  useSearchParams: () => new URLSearchParams(search.value),
}))
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => confirmFn }))
vi.mock("next-auth/react", () => ({ useSession: () => ({ update, data: session.value }) }))
vi.mock("@/components/account/switch-user", () => ({ signOutForSwitch }))
vi.mock("./my-shop-name", () => ({ fetchMyShopName }))

import { JoinClient } from "./join-client"

describe("JoinClient", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    search.value = "token=abc"
    update.mockResolvedValue(null)
    session.value = null
    fetchMyShopName.mockResolvedValue("我的店")
  })

  it("joins with the token, refreshes the session and shows the real shop and role from the server", async () => {
    // The link says clerk, the server says cashier: the server wins.
    search.value = "token=abc&shop=阿明五金&role=店员"
    joinShop.mockResolvedValue({ tenant_id: "t", shop_name: "阿明五金", role: "cashier", role_label: "收银" })
    render(<JoinClient />)
    fireEvent.click(screen.getByRole("button", { name: "加入店铺" }))
    await waitFor(() => expect(joinShop).toHaveBeenCalledWith("abc"))
    expect(await screen.findByText("你已加入「阿明五金」，角色：收银")).toBeInTheDocument()
    expect(update).toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "进入店铺" }))
    expect(replace).toHaveBeenCalledWith("/dashboard")
  })

  it("shows the shop name and role from the link before joining", () => {
    search.value = "token=abc&shop=阿明五金&role=店员"
    render(<JoinClient />)
    expect(screen.getByText(/阿明五金/)).toBeInTheDocument()
    expect(screen.getByText(/店员/)).toBeInTheDocument()
  })

  it("tells the person to use an account without a shop when they already own a non-empty one", async () => {
    joinShop.mockRejectedValue(
      new ApiError(409, "already_in_shop", "你的账号已经开过自己的店。请换一个没开过店的账号来加入，或联系客服。", {
        message: "你的账号已经开过自己的店。请换一个没开过店的账号来加入，或联系客服。",
        can_abandon_empty_shop: false,
      }),
    )
    render(<JoinClient />)
    fireEvent.click(screen.getByRole("button", { name: "加入店铺" }))
    expect(await screen.findByText(/请换一个没开过店的账号/)).toBeInTheDocument()
    expect(confirmFn).not.toHaveBeenCalled()
  })

  it("offers to abandon an empty shop, explains the consequence, and retries with the confirmation", async () => {
    joinShop
      .mockRejectedValueOnce(
        new ApiError(409, "already_in_shop", "你的账号已经开过自己的店。", { can_abandon_empty_shop: true }),
      )
      .mockResolvedValueOnce({ tenant_id: "t", shop_name: "阿明五金", role: "clerk", role_label: "店员" })
    confirmFn.mockResolvedValue(true)
    render(<JoinClient />)
    fireEvent.click(screen.getByRole("button", { name: "加入店铺" }))
    await waitFor(() => expect(confirmFn).toHaveBeenCalled())
    const opts = confirmFn.mock.calls[0][0]
    expect(opts.danger).toBe(true)
    expect(opts.body).toMatch(/只有系统自带的示例数据/)
    expect(opts.body).toMatch(/不能恢复|无法恢复/)
    await waitFor(() => expect(joinShop).toHaveBeenLastCalledWith("abc", { abandonEmptyShop: true }))
    expect(await screen.findByText(/你已加入「阿明五金」，角色：店员/)).toBeInTheDocument()
  })

  it("shows the own-shop notice, not an error, when the retry after abandoning says already_member_here", async () => {
    joinShop
      .mockRejectedValueOnce(
        new ApiError(409, "already_in_shop", "你的账号已经开过自己的店。", { can_abandon_empty_shop: true }),
      )
      .mockRejectedValueOnce(new ApiError(409, "already_member_here", "这是你自己店铺的邀请链接"))
    confirmFn.mockResolvedValue(true)
    render(<JoinClient />)
    fireEvent.click(screen.getByRole("button", { name: "加入店铺" }))
    expect(await screen.findByText(/这是你自己店的邀请链接/)).toBeInTheDocument()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("does nothing more when the person declines to abandon the empty shop", async () => {
    joinShop.mockRejectedValue(
      new ApiError(409, "already_in_shop", "你的账号已经开过自己的店。", { can_abandon_empty_shop: true }),
    )
    confirmFn.mockResolvedValue(false)
    render(<JoinClient />)
    fireEvent.click(screen.getByRole("button", { name: "加入店铺" }))
    await waitFor(() => expect(confirmFn).toHaveBeenCalled())
    expect(joinShop).toHaveBeenCalledTimes(1)
    expect(replace).not.toHaveBeenCalled()
  })

  it("tells the person to ask for a new link when the token is missing", () => {
    search.value = ""
    render(<JoinClient />)
    expect(screen.getByText(/邀请链接不完整/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "加入店铺" })).toBeNull()
  })

  it("shows the server's Chinese message when the invite is no longer valid", async () => {
    joinShop.mockRejectedValue(new ApiError(410, "invite_invalid", "这个邀请已失效（过期、已被使用或已被撤销）"))
    render(<JoinClient />)
    fireEvent.click(screen.getByRole("button", { name: "加入店铺" }))
    expect(await screen.findByText(/这个邀请已失效/)).toBeInTheDocument()
    expect(replace).not.toHaveBeenCalled()
  })

  it("shows who is signed in and switches account back to this very invite link", async () => {
    session.value = { user: { name: "小李", tenantId: "t1", role: "owner", isOwner: true } }
    search.value = "token=abc&i=inv1&shop=阿明五金"
    render(<JoinClient />)
    expect(await screen.findByText("当前登录：小李（我的店的老板）")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "不是你？换个账号" }))
    expect(signOutForSwitch).toHaveBeenCalledTimes(1)
    const [, role, returnTo] = signOutForSwitch.mock.calls[0]
    expect(role).toBe("owner")
    expect(returnTo).toBe("/join?token=abc&i=inv1&shop=%E9%98%BF%E6%98%8E%E4%BA%94%E9%87%91")
  })

  it("hides the join button when the link is for the person's own shop", async () => {
    session.value = { user: { name: "小李", tenantId: "t1", role: "owner", isOwner: true } }
    search.value = "token=abc&shop=我的店"
    render(<JoinClient />)
    expect(await screen.findByText(/这是你自己店的邀请链接，请发给对方，用他自己的手机和账号打开/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "加入店铺" })).toBeNull()
  })

  it("sends the invite id and, on already_member_here, shows the notice without any abandon dialog", async () => {
    session.value = { user: { name: "小李", tenantId: "t1", role: "owner", isOwner: true } }
    fetchMyShopName.mockResolvedValue("")
    search.value = "token=abc&i=inv1"
    joinShop.mockRejectedValue(new ApiError(409, "already_member_here", "这是你自己店铺的邀请链接"))
    render(<JoinClient />)
    fireEvent.click(screen.getByRole("button", { name: "加入店铺" }))
    expect(await screen.findByText(/这是你自己店的邀请链接/)).toBeInTheDocument()
    expect(joinShop).toHaveBeenCalledWith("abc", { inviteId: "inv1" })
    expect(confirmFn).not.toHaveBeenCalled()
    expect(screen.queryByRole("button", { name: "加入店铺" })).toBeNull()
  })

  it("does not show a login line when nobody is known yet", () => {
    render(<JoinClient />)
    expect(screen.queryByText(/当前登录/)).toBeNull()
  })

  it("still shows the shop's own name when no print name was ever filled in", async () => {
    // The server's /me already falls back to the setup name, so the page never shows a bare role.
    session.value = { user: { name: "小李", tenantId: "t1", role: "owner", isOwner: true } }
    fetchMyShopName.mockResolvedValue("开店时的店名")
    render(<JoinClient />)
    expect(await screen.findByText("当前登录：小李（开店时的店名的老板）")).toBeInTheDocument()
  })

  it("trusts the server over the shop name when the link carries an invite id", async () => {
    session.value = { user: { name: "小李", tenantId: "t1", role: "owner", isOwner: true } }
    search.value = "token=abc&i=inv1&shop=我的店"
    render(<JoinClient />)
    expect(await screen.findByText(/当前登录/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "加入店铺" })).toBeInTheDocument()
  })

  it("compares names trimmed and cut to 40 characters when the link has no invite id", async () => {
    const long = "长".repeat(45)
    session.value = { user: { name: "小李", tenantId: "t1", role: "owner", isOwner: true } }
    search.value = `token=abc&shop=${encodeURIComponent(" " + long)}`
    fetchMyShopName.mockResolvedValue(long)
    render(<JoinClient />)
    expect(await screen.findByText(/这是你自己店的邀请链接/)).toBeInTheDocument()
  })
})
