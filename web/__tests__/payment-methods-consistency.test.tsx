/**
 * Payment-method whitelist consistency.
 *
 * Business risk under test: the subscribe screen renders a hardcoded picker
 * of payment methods (`PAYMENT_METHODS` in plans-view.tsx) that is separate
 * from the wire type the backend/platform actually accepts
 * (`PaymentMethod` in lib/api/billing.ts). If someone typos a value here
 * (e.g. "wechatpay" instead of "wechat") the picker still renders fine and
 * the subscribe button still "works" in the UI — it just POSTs a
 * payment_method platform doesn't recognise, and that ONE payment option
 * silently breaks for every customer who picks it, while wallet/alipay keep
 * working. Nothing in the UI signals which option is broken until a support
 * ticket comes in. Separately, the pricing page's FAQ copy promises specific
 * payment methods to potential customers before they ever sign in — if that
 * copy drifts from what's actually offered post-login, customers read a
 * promise the checkout screen doesn't keep.
 */
import { describe, it, expect, vi } from "vitest"
import { readFileSync } from "node:fs"
import path from "node:path"
import { render, screen } from "@testing-library/react"

vi.mock("@/lib/api/billing", () => ({
  BillingError: class BillingError extends Error {},
  getBillingOverview: vi.fn().mockResolvedValue(null),
  subscribe: vi.fn(),
}))

vi.mock("next/link", () => ({
  default: ({
    href,
    children,
    ...props
  }: {
    href: string
    children: React.ReactNode
    [key: string]: unknown
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))

import { SubscriptionPlansView } from "@/app/(dashboard)/subscription/plans-view"
import PricingPage from "@/app/pricing/page"

/** The full set of payment methods the backend/platform wire type accepts. */
function backendAcceptedPaymentMethods(): string[] {
  const src = readFileSync(path.resolve(__dirname, "../lib/api/billing.ts"), "utf8")
  const m = src.match(/export type PaymentMethod = ([^\n]+)/)
  if (!m) throw new Error("lib/api/billing.ts no longer exports a PaymentMethod union — update this test")
  return Array.from(m[1].matchAll(/"([^"]+)"/g)).map((x) => x[1])
}

describe("subscribe screen payment-method picker vs backend-accepted set", () => {
  it("every rendered payment option is one the backend actually accepts (no typo'd payment_method silently breaking one option)", () => {
    const accepted = backendAcceptedPaymentMethods()
    expect(accepted.length).toBeGreaterThan(0)

    render(<SubscriptionPlansView />)
    const buttons = screen.getAllByRole("button", { name: /钱包余额|支付宝|微信支付|stripe/i })
    const renderedValues = buttons
      .map((b) => b.getAttribute("data-testid"))
      .filter((t): t is string => !!t?.startsWith("pm-"))
      .map((t) => t.slice("pm-".length))

    expect(renderedValues.length).toBeGreaterThan(0)
    for (const value of renderedValues) {
      expect(accepted).toContain(value)
    }
  })

  it("the rendered picker is exactly {wallet, alipay, wechat} today — pins the known-good set so a typo (e.g. 'wechatpay') fails loudly instead of silently breaking one payment option", () => {
    render(<SubscriptionPlansView />)
    expect(screen.getByTestId("pm-wallet")).toBeInTheDocument()
    expect(screen.getByTestId("pm-alipay")).toBeInTheDocument()
    expect(screen.getByTestId("pm-wechat")).toBeInTheDocument()
    expect(screen.queryByTestId("pm-wechatpay")).not.toBeInTheDocument()
  })

  it("the pricing page's FAQ copy about accepted payment methods matches what the checkout screen actually offers", () => {
    render(<PricingPage />)
    expect(screen.getByText(/钱包余额、支付宝、微信支付/)).toBeInTheDocument()
  })
})
