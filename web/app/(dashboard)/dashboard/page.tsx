import Link from "next/link"
import { auth } from "@/auth"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default async function DashboardPage({
  searchParams,
}: {
  searchParams?: { error?: string }
}) {
  const session = await auth()
  const profileType = session?.user?.profileType
  const error = searchParams?.error

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
        <header className="space-y-1">
          <h1 className="text-2xl font-bold tracking-tight">欢迎回到 Lurus Tally</h1>
          <p className="text-sm text-muted-foreground">
            {profileType === "cross_border"
              ? "跨境贸易模式 · FIFO 批次计价"
              : profileType === "retail"
                ? "零售/批发模式 · WAC 加权平均"
                : "选择左侧任一模块开始。"}
          </p>
        </header>

        {error === "pos-retail-only" ? (
          <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
            POS 收银仅对零售模式开放。
          </div>
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
      </div>
    </main>
  )
}
