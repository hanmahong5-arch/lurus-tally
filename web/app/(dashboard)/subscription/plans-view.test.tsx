/**
 * SubscriptionPlansView — checkout hand-off tests.
 *
 * Business risk under test: the pricing → login → subscribe funnel's LAST
 * step is a client-side branch on the backend's response shape. If that
 * branch regresses, a customer can click "订阅", pick 支付宝/微信支付, and
 * the app just shows a generic "已提交订阅请求" toast without ever sending
 * their browser to the actual payment page — they never get a chance to
 * pay, and nothing in the UI looks broken (no error, no crash). Conversely,
 * if the wallet-activation branch is mistakenly treated as "needs redirect",
 * an already-paid customer gets bounced off the app on every subscribe click.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

const seed = vi.hoisted(() => ({ on: false }))
vi.mock("@/lib/billing/plans", async (orig) => {
  const actual = await orig<typeof import("@/lib/billing/plans")>()
  return {
    ...actual,
    get SEED_TRIAL() {
      return seed.on
    },
  }
})

vi.mock("@/lib/api/billing", () => ({
  BillingError: class BillingError extends Error {
    code: string
    httpStatus: number
    constructor(code: string, message: string, httpStatus: number) {
      super(message)
      this.code = code
      this.httpStatus = httpStatus
    }
  },
  getBillingOverview: vi.fn(),
  getSubscriptionState: vi.fn(() => Promise.resolve({ locked: false, refreshed: true })),
  getOrderStatus: vi.fn(),
  subscribe: vi.fn(),
}))

vi.mock("qrcode.react", () => ({
  QRCodeSVG: ({ value }: { value: string }) => <svg data-testid="pay-qr" data-value={value} />,
}))

import {
  getBillingOverview,
  getOrderStatus,
  getSubscriptionState,
  subscribe,
  BillingError,
} from "@/lib/api/billing"
import * as plans from "@/lib/billing/plans"
import { SubscriptionPlansView } from "./plans-view"

const baseOverview = {
  account_id: 1,
  wallet: { available: 500, frozen: 0, total: 500 },
  vip: { level: 0, level_name: "" },
  subscription: null,
  entitlements: { plan_code: "free" },
  can_manage: true,
}

describe("SubscriptionPlansView — checkout redirect hand-off", () => {
  const originalLocation = window.location

  beforeEach(() => {
    seed.on = false
    vi.clearAllMocks()
    // jsdom's window.location.assign is non-configurable via vi.spyOn; swap
    // the whole object out so we can observe navigation calls.
    Object.defineProperty(window, "location", {
      value: { ...originalLocation, assign: vi.fn() },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, "location", {
      value: originalLocation,
      writable: true,
      configurable: true,
    })
  })

  it("防止客户选了支付宝/微信支付却卡在原地——外部支付方式必须把浏览器带去 pay_url，而不是只弹一条'已提交'提示", async () => {
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview)
    vi.mocked(subscribe).mockResolvedValue({
      order_no: "ORD-1",
      pay_url: "https://alipay.example/qr/ORD-1",
    })

    render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())

    // Switch to 支付宝 before subscribing, mirroring a real checkout.
    fireEvent.click(screen.getByTestId("pm-alipay"))
    fireEvent.click(screen.getByTestId("subscribe-pro"))

    await waitFor(() => {
      expect(window.location.assign).toHaveBeenCalledWith("https://alipay.example/qr/ORD-1")
    })
    expect(subscribe).toHaveBeenCalledWith(
      expect.objectContaining({ plan_code: "pro", payment_method: "alipay" }),
    )
    // The customer has NOT paid yet — they're mid-redirect to the gateway.
    // Showing an "activated" success message here would be a lie.
    expect(screen.queryByText(/已开通/)).not.toBeInTheDocument()
  })

  it("防止钱包一键开通被错误当成外部支付重定向——已立即生效的订阅不应把用户踢出应用", async () => {
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview)
    vi.mocked(subscribe).mockResolvedValue({
      subscription: { plan_code: "pro", status: "active" },
    })

    render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())

    // Default payment method is wallet — subscribe without switching.
    fireEvent.click(screen.getByTestId("subscribe-pro"))

    await waitFor(() => {
      expect(screen.getByText(/已开通 专业版/)).toBeInTheDocument()
    })
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it("防止桌面端选微信支付后卡死——微信扫码要在页面里出二维码，并在支付到账后提示开通", async () => {
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview)
    vi.mocked(subscribe).mockResolvedValue({
      order_no: "ORD-QR",
      qr_code: "weixin://wxpay/bizpayurl?pr=abc",
    })
    vi.mocked(getOrderStatus).mockResolvedValue({ order_no: "ORD-QR", status: "paid", paid: true, final: true })

    render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())

    fireEvent.click(screen.getByTestId("pm-wechat"))
    fireEvent.click(screen.getByTestId("subscribe-pro"))

    await waitFor(() => expect(getOrderStatus).toHaveBeenCalledWith("ORD-QR"))
    await waitFor(() => expect(screen.getByText(/已开通 专业版/)).toBeInTheDocument())
    // A QR payment is never a redirect.
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it("防止成员误以为能升级——非企业所有者看到说明，订阅按钮不可点", async () => {
    vi.mocked(getBillingOverview).mockResolvedValue({ ...baseOverview, can_manage: false })

    render(<SubscriptionPlansView />)
    await waitFor(() => expect(screen.getByText(/只有企业所有者可以变更/)).toBeInTheDocument())
    expect(screen.getByTestId("subscribe-pro")).toBeDisabled()
  })

  it("没有订阅时显示免费版，不写未开通", async () => {
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview as never)
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())
    await waitFor(() => expect(container.textContent).toContain("免费版"))
    expect(container.textContent).not.toContain("未开通")
  })

  it("套餐和状态一律显示中文，不露出英文代码", async () => {
    vi.mocked(getBillingOverview).mockResolvedValue({
      ...baseOverview,
      subscription: { plan_code: "pro", status: "grace" },
    } as never)
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())
    await waitFor(() => expect(container.textContent).toContain("专业版（宽限期内）"))
    expect(container.textContent).not.toMatch(/grace|pro \(/)
  })
})

describe("SubscriptionPlansView — 种子期", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("种子期没有订阅按钮和支付方式，出现免费试用横幅，全文无'回调'", async () => {
    seed.on = true
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview as never)
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())
    expect(screen.queryAllByTestId(/^subscribe-/)).toHaveLength(0)
    expect(screen.queryByTestId("pm-alipay")).not.toBeInTheDocument()
    expect(screen.queryByRole("button")).not.toBeInTheDocument()
    expect(container.textContent).toContain("试用期全部功能免费，正式收费前会提前通知")
    expect(container.textContent).not.toContain("回调")
    expect(plans.TALLY_PLANS.length).toBeGreaterThan(0)
  })

  it("计费错误带英文 message 时显示中文通用文案", async () => {
    seed.on = false
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview as never)
    vi.mocked(subscribe).mockRejectedValue(new BillingError("weird", "upstream exploded 500", 500))
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())
    fireEvent.click(screen.getByTestId("subscribe-pro"))
    await waitFor(() => expect(container.textContent).toContain("订阅失败，请稍后再试。"))
    expect(container.textContent).not.toContain("upstream exploded")
  })

  it("断网类计费错误显示网络提示，不显示订阅失败", async () => {
    seed.on = false
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview as never)
    vi.mocked(subscribe).mockRejectedValue(new BillingError("network", "getBillingOverview: Failed to fetch", 0))
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())
    fireEvent.click(screen.getByTestId("subscribe-pro"))
    await waitFor(() => expect(container.textContent).toContain("网络不通，请检查网络后再试"))
    expect(container.textContent).not.toContain("订阅失败")
  })

  it("读概览失败只显示概览文案，不出现订阅失败，也不露英文", async () => {
    seed.on = true
    vi.mocked(getBillingOverview).mockRejectedValue(new BillingError("forbidden", "denied by upstream", 403))
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(container.textContent).toContain("暂时无法读取账户概览，请稍后再试"))
    expect(container.textContent).not.toContain("订阅失败")
    expect(container.textContent).not.toContain("denied")
  })

  it("读概览遇到断网显示网络提示", async () => {
    seed.on = true
    vi.mocked(getBillingOverview).mockRejectedValue(new BillingError("offline", "x", 0))
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(container.textContent).toContain("网络不通，请检查网络后再试"))
    expect(container.textContent).not.toContain("订阅失败")
  })

  it("种子期当前套餐的卡片上有当前套餐标记", async () => {
    seed.on = true
    vi.mocked(getBillingOverview).mockResolvedValue({
      ...baseOverview,
      subscription: { plan_code: plans.TALLY_PLANS[0].code, status: "active" },
    } as never)
    render(<SubscriptionPlansView />)
    expect(await screen.findByTestId(`current-plan-${plans.TALLY_PLANS[0].code}`)).toHaveTextContent("当前套餐")
  })

  it("种子期横幅只出现一次", async () => {
    seed.on = true
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview as never)
    const { container } = render(<SubscriptionPlansView />)
    await waitFor(() => expect(getBillingOverview).toHaveBeenCalled())
    expect(container.textContent?.split("试用期全部功能免费").length).toBe(2)
  })
})

describe("SubscriptionPlansView payment return", () => {
  it("refreshes the subscription state once when returning with paid=1", async () => {
    vi.mocked(getBillingOverview).mockResolvedValue(baseOverview as never)
    window.history.pushState({}, "", "/subscription?paid=1")
    render(<SubscriptionPlansView />)
    await waitFor(() => expect(getSubscriptionState).toHaveBeenCalledWith({ refresh: true }))
    window.history.pushState({}, "", "/")
  })
})

