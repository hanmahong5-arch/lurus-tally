import { SubscriptionPlansView } from "./plans-view"

/**
 * Tally subscription / billing page.
 *
 * One-click subscribe lives here. The Server Component shell renders the
 * static plan catalog; the client component below handles the actual
 * checkout call (POST /api/v1/billing/subscribe) and overview hydration.
 */
export default function SubscriptionPage() {
  return (
    <div className="mx-auto max-w-6xl px-6 py-8">
      <header className="mb-8">
        <h1 className="text-2xl font-semibold text-foreground">订阅与计费</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          选择适合企业规模的套餐，钱包余额可一键开通；支付宝 / 微信支付随后跳转。
        </p>
      </header>
      <SubscriptionPlansView />
    </div>
  )
}
