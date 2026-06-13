import Link from "next/link"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import type { WeeklySummary } from "@/lib/api/digest"

interface MondayCardProps {
  summary: WeeklySummary | null
}

/**
 * MondayCard — 本周经营摘要 ("Monday card").
 *
 * Renders three inventory signals with one-click CTAs:
 *   补货建议  → /replenish
 *   超卖风险  → /stock
 *   呆滞库存  → /reports
 *
 * Degrades gracefully: when summary is null the card is hidden entirely
 * so it never blocks the rest of the dashboard from loading.
 */
export function MondayCard({ summary }: MondayCardProps) {
  if (!summary) return null

  const { replenish, oversell, dead_stock } = summary

  // Only render the card when at least one signal is non-zero.
  const hasSignal = replenish.count > 0 || oversell.count > 0 || dead_stock.count > 0
  if (!hasSignal) return null

  const amountDisplay = Number(replenish.amount_cny).toLocaleString("zh-CN", {
    style: "currency",
    currency: "CNY",
    maximumFractionDigits: 0,
  })

  return (
    <Card className="border-amber-500/40 bg-amber-500/5">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <span aria-hidden="true">📋</span>
          本周经营摘要
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="divide-y divide-border">
          {replenish.count > 0 && (
            <li className="flex items-center justify-between py-2.5 gap-3">
              <div className="flex items-center gap-2 min-w-0">
                <Badge tone="warn" className="shrink-0">
                  {replenish.count} 项
                </Badge>
                <span className="text-sm">
                  建议补货 &mdash; 合计 {amountDisplay}
                </span>
              </div>
              <Link
                href="/replenish"
                className={buttonVariants({ size: "sm", variant: "outline" })}
              >
                查看
              </Link>
            </li>
          )}

          {oversell.count > 0 && (
            <li className="flex items-center justify-between py-2.5 gap-3">
              <div className="flex items-center gap-2 min-w-0">
                <Badge tone="err" className="shrink-0">
                  {oversell.count} 项
                </Badge>
                <span className="text-sm">超卖风险 &mdash; 可用量为负</span>
              </div>
              <Link
                href="/stock"
                className={buttonVariants({ size: "sm", variant: "outline" })}
              >
                查看
              </Link>
            </li>
          )}

          {dead_stock.count > 0 && (
            <li className="flex items-center justify-between py-2.5 gap-3">
              <div className="flex items-center gap-2 min-w-0">
                <Badge tone="neutral" className="shrink-0 text-muted-foreground">
                  {dead_stock.count} 项
                </Badge>
                <span className="text-sm">呆滞库存 &mdash; 90 天无动销</span>
              </div>
              <Link
                href="/reports"
                className={buttonVariants({ size: "sm", variant: "outline" })}
              >
                查看
              </Link>
            </li>
          )}
        </ul>
      </CardContent>
    </Card>
  )
}
