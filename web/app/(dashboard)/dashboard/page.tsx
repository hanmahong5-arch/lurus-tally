import Link from "next/link"
import { auth } from "@/auth"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorBanner } from "@/components/ui/error-banner"
import { buttonVariants } from "@/components/ui/button"
import { fetchLowStockAlerts, fetchDraftPurchaseBillCount } from "@/lib/api/stock"
import { fetchWeeklySummary } from "@/lib/api/digest"
import { MondayCard } from "@/components/dashboard/monday-card"

export const revalidate = 60

export default async function DashboardPage({
  searchParams,
}: {
  searchParams?: { error?: string }
}) {
  const session = await auth()
  const profileType = session?.user?.profileType
  const error = searchParams?.error

  // Fetch dashboard widget data server-side; degrade gracefully on failure.
  const accessToken = session?.accessToken ?? ""
  const [lowStock, draftPurchaseCount, weeklySummary] = await Promise.all([
    accessToken ? fetchLowStockAlerts(accessToken, 5) : Promise.resolve({ items: [], count: 0 }),
    accessToken ? fetchDraftPurchaseBillCount(accessToken) : Promise.resolve(0),
    accessToken ? fetchWeeklySummary(accessToken) : Promise.resolve(null),
  ])

  const cards: { href: string; title: string; description: string; emoji: string }[] = [
    { href: "/products", title: "商品管理", description: "SKU、单位、分类、价格", emoji: "📦" },
    { href: "/purchases", title: "采购管理", description: "进货单、入库、批次", emoji: "🛒" },
    { href: "/sales", title: "销售管理", description: "销售单、出库、对账", emoji: "📊" },
    { href: "/finance/exchange-rates", title: "财务管理", description: "汇率、币种、成本", emoji: "💰" },
    { href: "/subscription", title: "订阅与计费", description: "套餐、钱包、账单", emoji: "💳" },
  ]
  if (profileType === "retail") {
    cards.unshift({ href: "/pos", title: "POS 收银", description: "门店即时收银", emoji: "🖥️" })
  }

  return (
    <main className="flex-1 overflow-y-auto px-6 py-8">
      <div className="mx-auto max-w-5xl space-y-6">
        {/* Weekly summary Monday card — mounts at top; hidden when no signals */}
        <MondayCard summary={weeklySummary} />

        <header className="space-y-1">
          <h1 className="text-2xl font-bold tracking-tight">欢迎回到 Lurus Tally</h1>
          <p className="text-sm text-muted-foreground">
            {profileType === "cross_border"
              ? "跨境贸易模式 · FIFO 批次计价"
              : profileType === "retail"
                ? "零售/批发模式 · WAC 加权平均"
                : "选择左侧任一模块开始。"}
          </p>
          <p className="pt-1 text-xs text-muted-foreground/80">
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px]">⌘K</kbd>
            <span className="ml-1">快速搜索任意页面</span>
            <span className="mx-2 opacity-50">·</span>
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px]">⌘J</kbd>
            <span className="ml-1">召唤 AI 助手</span>
          </p>
        </header>

        {error === "pos-retail-only" ? (
          <ErrorBanner>POS 收银仅对零售模式开放。</ErrorBanner>
        ) : null}

        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {cards.map((c) => (
            <Link key={c.href} href={c.href}>
              <Card className="h-full transition-colors hover:bg-muted/50">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base">
                    <span aria-hidden="true">{c.emoji}</span>
                    {c.title}
                  </CardTitle>
                  <CardDescription>{c.description}</CardDescription>
                </CardHeader>
                <CardContent />
              </Card>
            </Link>
          ))}
        </section>

        {/* Intelligence widgets */}
        <section className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {/* Low-stock TOP 5 */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-base">低库存预警 TOP 5</CardTitle>
              <CardDescription>可用量低于补货点的商品</CardDescription>
            </CardHeader>
            <CardContent>
              {lowStock.items.length === 0 ? (
                <p className="text-sm text-muted-foreground py-4 text-center">
                  暂无低库存商品
                </p>
              ) : (
                <ul className="divide-y divide-border">
                  {lowStock.items.map((item) => (
                    <li
                      key={item.product_id}
                      className="flex items-center justify-between py-2.5 gap-3"
                    >
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium truncate">{item.product_name}</p>
                        <p className="text-xs text-muted-foreground truncate">
                          约 {Number(item.days_of_supply).toFixed(0)} 天可售
                        </p>
                      </div>
                      <div className="text-right flex-shrink-0">
                        <p className="text-sm font-mono tabular-nums text-destructive">
                          {Number(item.available_qty).toFixed(0)}
                          <span className="text-muted-foreground text-xs"> / {Number(item.reorder_point).toFixed(0)}</span>
                        </p>
                        <Link
                          href={`/purchases/new?prefill_product_id=${item.product_id}`}
                          className="text-xs text-primary hover:underline"
                        >
                          下采购单
                        </Link>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
              {lowStock.count > 5 && (
                <p className="mt-2 text-xs text-muted-foreground text-center">
                  还有 {lowStock.count - 5} 个商品低于安全库存
                </p>
              )}
            </CardContent>
          </Card>

          {/* Draft purchase bills count */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-base">待审单据</CardTitle>
              <CardDescription>需要审核的草稿采购单</CardDescription>
            </CardHeader>
            <CardContent>
              {draftPurchaseCount === 0 ? (
                <p className="text-sm text-muted-foreground py-4 text-center">
                  暂无待审采购单
                </p>
              ) : (
                <div className="flex items-center justify-between py-2">
                  <div>
                    <p className="text-3xl font-bold tabular-nums">{draftPurchaseCount}</p>
                    <p className="text-sm text-muted-foreground mt-0.5">张待审采购单</p>
                  </div>
                  <Link href="/purchases?status=draft" className={buttonVariants()}>
                    去审核
                  </Link>
                </div>
              )}
            </CardContent>
          </Card>
        </section>
      </div>
    </main>
  )
}
