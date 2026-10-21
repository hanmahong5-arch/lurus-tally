import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

const { state } = vi.hoisted(() => ({
  state: { role: "owner" as string, overview: { locked: true } as { locked?: boolean } | Error },
}))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role } } }),
}))
vi.mock("next/link", () => ({
  default: ({ href, children, ...p }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...p}>
      {children}
    </a>
  ),
}))
const { toastMock, refreshMock } = vi.hoisted(() => ({
  toastMock: { success: vi.fn(), info: vi.fn(), error: vi.fn() },
  refreshMock: vi.fn(),
}))
vi.mock("sonner", () => ({ toast: toastMock }))
vi.mock("@/lib/api/billing", () => ({
  getSubscriptionState: refreshMock,
  getSubscriptionLockState: () =>
    state.overview instanceof Error ? Promise.reject(state.overview) : Promise.resolve(state.overview),
}))

import { SubscriptionBanner } from "./subscription-banner"

describe("SubscriptionBanner", () => {
  beforeEach(() => {
    state.role = "owner"
    state.overview = { locked: true }
    vi.clearAllMocks()
  })

  it("refresh button hides the banner once the plan is back", async () => {
    refreshMock.mockResolvedValue({ locked: false, refreshed: true })
    render(<SubscriptionBanner />)
    fireEvent.click(await screen.findByRole("button", { name: "我已续费，刷新状态" }))
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
    expect(refreshMock).toHaveBeenCalledWith({ refresh: true })
    expect(toastMock.success).toHaveBeenCalledWith("套餐已恢复")
  })

  it("refresh button keeps the banner when still locked", async () => {
    refreshMock.mockResolvedValue({ locked: true, refreshed: true })
    render(<SubscriptionBanner />)
    fireEvent.click(await screen.findByRole("button", { name: "我已续费，刷新状态" }))
    await waitFor(() => expect(toastMock.info).toHaveBeenCalled())
    expect(screen.getByRole("alert")).toBeInTheDocument()
  })

  it("owner sees a renew button to the subscription page", async () => {
    render(<SubscriptionBanner />)
    const link = await screen.findByRole("link", { name: "续费" })
    expect(link.getAttribute("href")).toBe("/subscription")
    expect(screen.getByText(/套餐已到期，现在只能查看和导出/)).toBeInTheDocument()
    expect(screen.queryByText("请联系老板续费")).toBeNull()
  })

  it("clerk is told to contact the owner and gets no renew button", async () => {
    state.role = "clerk"
    render(<SubscriptionBanner />)
    expect(await screen.findByText("请联系老板续费")).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "续费" })).toBeNull()
  })

  it("shows nothing when not locked or when the status cannot be read", async () => {
    state.overview = { locked: false }
    const { container, rerender } = render(<SubscriptionBanner />)
    await waitFor(() => expect(container.firstChild).toBeNull())
    state.overview = new Error("boom")
    rerender(<SubscriptionBanner />)
    expect(container.firstChild).toBeNull()
  })
})
