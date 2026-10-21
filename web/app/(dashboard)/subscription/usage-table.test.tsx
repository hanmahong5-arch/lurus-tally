/**
 * UsageTable —— 订阅页「用量」小表。
 * 业务风险：上限 -1 若被当数字显示成「-1 个」会吓到店主；取不到 limits 时不该渲染空表。
 */
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

vi.mock("@/lib/api/billing", () => ({
  getSubscriptionLockState: vi.fn(),
}))

import { getSubscriptionLockState } from "@/lib/api/billing"
import { UsageTable } from "./usage-table"

describe("UsageTable", () => {
  beforeEach(() => {
    vi.mocked(getSubscriptionLockState).mockReset()
  })

  it("shows used and limit, with -1 rendered as 不限", async () => {
    vi.mocked(getSubscriptionLockState).mockResolvedValue({
      locked: false,
      limits: {
        max_users: { limit: 3, used: 2, enforced: false },
        max_skus: { limit: -1, used: 120, enforced: false },
        max_warehouses: { limit: 1, used: 1, enforced: false },
      },
    })
    render(<UsageTable />)
    await waitFor(() => expect(screen.getByText("用量")).toBeTruthy())
    expect(screen.getByText("成员")).toBeTruthy()
    expect(screen.getByText("2 人")).toBeTruthy()
    expect(screen.getByText("3 人")).toBeTruthy()
    expect(screen.getByText("120 个")).toBeTruthy()
    expect(screen.getByText("不限")).toBeTruthy()
    expect(screen.queryByText(/-1/)).toBeNull()
  })

  it("renders nothing when the state carries no limits", async () => {
    vi.mocked(getSubscriptionLockState).mockResolvedValue({ locked: false })
    const { container } = render(<UsageTable />)
    await waitFor(() => expect(getSubscriptionLockState).toHaveBeenCalled())
    expect(container.innerHTML).toBe("")
  })

  it("renders nothing when the request fails", async () => {
    vi.mocked(getSubscriptionLockState).mockRejectedValue(new Error("net"))
    const { container } = render(<UsageTable />)
    await waitFor(() => expect(getSubscriptionLockState).toHaveBeenCalled())
    expect(container.innerHTML).toBe("")
  })
})
