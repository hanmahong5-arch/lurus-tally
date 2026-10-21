import { SubscriptionPlansView } from "./plans-view"
import { UsageTable } from "./usage-table"
import { SEED_TRIAL } from "@/lib/billing/plans"
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
        subtitle={
          SEED_TRIAL
            ? "试用期全部功能免费"
            : "选择适合你的套餐，钱包余额可一键开通；支付宝 / 微信支付随后跳转。"
        }
      />
      <SubscriptionPlansView />
      <UsageTable />
    </PageContainer>
  )
}
