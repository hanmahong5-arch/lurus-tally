import { SubscriptionPlansView } from "./plans-view"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"

/**
 * Tally subscription / billing page.
 *
 * One-click subscribe lives here. The Server Component shell renders the
 * static plan catalog; the client component below handles the actual
 * checkout call (POST /api/v1/billing/subscribe) and overview hydration.
 */
export default function SubscriptionPage() {
  return (
    <PageContainer width="wide">
      <PageHeader
        title="订阅与计费"
        subtitle="选择适合企业规模的套餐，钱包余额可一键开通；支付宝 / 微信支付随后跳转。"
      />
      <SubscriptionPlansView />
    </PageContainer>
  )
}
