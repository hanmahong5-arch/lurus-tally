/**
 * ConvertedNotice — shown after clearing the sample data when some sample
 * products are still used by approved bills: they stay as real products, and
 * the owner is asked to recount them.
 */

import type { ConvertedProduct } from "@/lib/api/onboarding"

const SHOWN = 5

export function ConvertedNotice({ products }: { products: ConvertedProduct[] }) {
  const names = products.slice(0, SHOWN).map((p) => p.name).join("、")
  const rest = products.length > SHOWN ? ` 等 ${products.length} 个商品` : ""
  return (
    <div role="status" className="rounded-lg border border-border bg-muted/50 px-4 py-3 text-sm space-y-1">
      <p className="font-medium">示例数据已清除</p>
      <p className="text-muted-foreground">
        {names}
        {rest}已经用在你审核过的单据里，已转成真实商品保留。请用盘点核对它们的数量和进价。
      </p>
    </div>
  )
}
