import Link from "next/link"
import { redirect } from "next/navigation"
import { auth } from "@/auth"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import { fetchWeeklySummary } from "@/lib/api/digest"
import { fetchTodoSummary, TODO_SOURCE_KPIS, type TodoSummary } from "@/lib/api/todo"
import { formatCNY } from "@/lib/format"
import { normalizeRole, roleCan, type Perm } from "@/lib/roles"
import { MondayCard } from "@/components/dashboard/monday-card"
import { ClearDemoCard } from "@/components/onboarding/ClearDemoCard"
import { CashierModeControl, HideInCashierMode } from "@/components/cashier-mode-gate"

export const revalidate = 60

interface FocusRow {
  key: string
  label: string
  count: number
  href: string
  action: string
  /** The action button only shows to people holding this permission. */
  perm: Perm
}

function focusRows(s: TodoSummary, can: (p: Perm) => boolean): FocusRow[] {
  const rows: FocusRow[] = [
    { key: "draft_sales", label: "张销售单等你审核", count: s.draft_sales.count, href: "/sales?status=0&range=all", action: "去审核", perm: "bill.approve" },
    { key: "draft_purchases", label: "张采购单等你审核", count: s.draft_purchases.count, href: "/purchases?status=0&range=all", action: "去审核", perm: "bill.approve" },
    { key: "overdue", label: "位客户欠款超过 30 天", count: s.overdue_customers.count, href: "/customers", action: "去收款", perm: "payment.record" },
    { key: "uninvoiced", label: "张销售单还没开足发票", count: s.uninvoiced_sales.count, href: "/sales?inv=none&range=all", action: "去开票", perm: "invoice.manage" },
    { key: "low_stock", label: "个商品库存偏低", count: s.low_stock.count, href: "/replenish", action: "去补货", perm: "replenish.view" },
  ]
  return rows.filter((r) => r.count > 0 && can(r.perm))
}

const TODO_SOURCE_ONBOARDING = "onboarding"

interface StartStep {
  key: "product" | "purchase" | "sale"
  text: string
  href: string
  action: string
  done: boolean
}

/** The three getting-started steps, ticked from the tenant's own records (sample data excluded). */
function startSteps(s: TodoSummary): StartStep[] {
  const ob = s.onboarding
  return [
    { key: "product", text: "添加你卖的商品", href: "/products", action: "去添加", done: !!ob?.has_product },
    { key: "purchase", text: "导入商品和期初库存（或记一笔进货）", href: "/products?import=1", action: "去导入", done: !!ob?.has_purchase },
    { key: "sale", text: "开出第一张销售单", href: "/sales/new", action: "去开单", done: !!ob?.has_sale },
  ]
}

export default async function DashboardPage({
  searchParams,
}: {
  searchParams?: { error?: string }
}) {
  const session = await auth()
  const profileType = session?.user?.profileType
  // What this person may see. An unknown role hides nothing (the backend still decides).
  const role = normalizeRole(session?.user?.role, session?.user?.isOwner === true)
  const can = (p: Perm) => role === null || roleCan(role, p)
  // A cashier works at the till: the back-office home is not for them.
  if (role === "cashier" && profileType === "retail") redirect("/pos")
  const error = searchParams?.error

  // Fetch dashboard widget data server-side; degrade gracefully on failure.
  const accessToken = session?.accessToken ?? ""
  const [todo, weeklySummary] = await Promise.all([
    fetchTodoSummary(accessToken),
    accessToken && can("report.view") ? fetchWeeklySummary(accessToken) : Promise.resolve(null),
  ])

  const kpisOk = !!todo && !todo.failed.includes(TODO_SOURCE_KPIS)
  const kpi = (v: string | undefined) => (kpisOk && v !== undefined ? formatCNY(v) : "—")
  // Money in and owed belongs to people who may read payments; anyone who may read sales sees today's sales (owner decision 2026-10-12).
  const kpiCards = [
    { label: "今日销售", value: kpi(todo?.kpis.today_sales), href: "/sales?range=today&status=2", perm: "bill.read" as Perm },
    { label: "今日回款", value: kpi(todo?.kpis.today_collected), href: "/payments", perm: "payment.read" as Perm },
    { label: "应收未收", value: kpi(todo?.kpis.receivable_unpaid), href: "/customers", perm: "payment.read" as Perm },
    { label: "本月未开票", value: kpi(todo?.kpis.month_uninvoiced), href: "/sales?inv=none&range=this_month", perm: "payment.read" as Perm },
  ].filter((c) => can(c.perm))

  const rows = todo ? focusRows(todo, can) : []
  const partial = !!todo && todo.failed.length > 0
  // Only a failed onboarding read hides the checklist; another source failing says nothing about it.
  const onboardingOk = !!todo && !todo.failed.includes(TODO_SOURCE_ONBOARDING) && !!todo.onboarding
  const steps = todo && onboardingOk ? startSteps(todo) : []
  const stepsDone = steps.filter((x) => x.done).length
  // Hidden only once all three steps are really done; sample data never counts.
  // Setting the shop up is the boss's and manager's job.
  const showSteps = can("settings.manage") && steps.length > 0 && stepsDone < steps.length
  const showDemoBanner = can("settings.manage") && !!todo && onboardingOk && todo.onboarding?.has_demo === true

  return (
    <main className="flex-1 overflow-y-auto px-6 py-8">
      <div className="mx-auto max-w-5xl space-y-6">
        {/* Weekly summary Monday card — mounts at top; hidden when no signals */}
        <HideInCashierMode retailOnly>
          <MondayCard summary={weeklySummary} />
        </HideInCashierMode>

        <header className="space-y-1">
          <h1 className="text-2xl font-bold tracking-tight">今日要事</h1>
          <p className="text-sm text-muted-foreground">
            {profileType === "cross_border"
              ? "跨境贸易模式 · 成本按批次先进先出"
              : profileType === "retail"
                ? "零售/批发模式 · 成本按进货均价"
                : "先看数字，再处理下面待办。"}
          </p>
          <p className="pt-1 text-xs text-muted-foreground/80">
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px]">⌘K</kbd>
            <span className="ml-1">快速搜索任意页面</span>
            {todo?.ai_enabled && can("ai.use") ? (
              <>
                <span className="mx-2 opacity-50">·</span>
                <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px]">⌘J</kbd>
                <span className="ml-1">召唤 AI 助手</span>
              </>
            ) : null}
          </p>
        </header>

        {error === "pos-retail-only" ? (
          <ErrorBanner>收银台仅对零售模式开放。</ErrorBanner>
        ) : null}

        {!todo ? (
          <ErrorBanner hint="请稍后刷新页面再试">首页数据加载失败</ErrorBanner>
        ) : partial ? (
          <ErrorBanner hint="下面的数字可能不全，请稍后刷新页面再试">部分数据加载失败</ErrorBanner>
        ) : null}

        {showDemoBanner ? (
          <HideInCashierMode retailOnly>
            <ClearDemoCard />
          </HideInCashierMode>
        ) : null}

        {/* Only shops that can reach the POS may switch it on. */}
        {profileType === "retail" && can("settings.manage") ? <CashierModeControl /> : null}

        <HideInCashierMode retailOnly>
          {kpiCards.length > 0 && (
            <section aria-label="经营数字" className="grid grid-cols-2 gap-4 lg:grid-cols-4">
              {kpiCards.map((c) => (
                <Link key={c.label} href={c.href}>
                  <Card className="h-full transition-colors hover:bg-muted/50">
                    <CardHeader className="pb-1">
                      <CardDescription>{c.label}</CardDescription>
                    </CardHeader>
                    <CardContent>
                      <p className="text-2xl font-bold tabular-nums">{c.value}</p>
                    </CardContent>
                  </Card>
                </Link>
              ))}
            </section>
          )}

          {showSteps ? (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-base">新手三步，开始记第一笔账（{stepsDone}/{steps.length}）</CardTitle>
                <CardDescription>做完这三步，这里就会出现你的经营数字和待办。</CardDescription>
              </CardHeader>
              <CardContent>
                <ol className="divide-y divide-border">
                  {steps.map((s, i) => (
                    <li key={s.key} className="flex items-center justify-between gap-3 py-3">
                      <span className="flex items-center gap-3 text-sm">
                        <Badge tone={s.done ? "ok" : "accent"}>{s.done ? "✓" : i + 1}</Badge>
                        <span className={s.done ? "text-muted-foreground line-through" : undefined}>{s.text}</span>
                      </span>
                      {s.done ? (
                        <span className="text-xs text-muted-foreground">已完成</span>
                      ) : (
                        <Link href={s.href} className={buttonVariants({ size: "sm" })}>
                          {s.action}
                        </Link>
                      )}
                    </li>
                  ))}
                </ol>
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-base">待你处理</CardTitle>
              <CardDescription>
                {todo ? (rows.length > 0 ? "按重要程度排好了，点右边按钮直接处理" : "") : ""}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {!todo ? (
                <p className="py-6 text-center text-sm text-muted-foreground">暂时无法获取待办事项</p>
              ) : rows.length === 0 ? (
                partial ? (
                  <p className="py-6 text-center text-sm text-muted-foreground">
                    暂时看不到完整的待办，请稍后刷新
                  </p>
                ) : (
                  <div className="flex flex-col items-center gap-1.5 py-6 text-center">
                    <span aria-hidden="true" className="text-xl opacity-60">✅</span>
                    <p className="text-sm text-muted-foreground">暂时没有需要处理的事项</p>
                  </div>
                )
              ) : (
                <ul className="divide-y divide-border">
                  {rows.map((r) => (
                    <li key={r.key} className="flex items-center justify-between gap-3 py-3">
                      <p className="text-sm">
                        <span className="mr-1 text-lg font-bold tabular-nums">{r.count}</span>
                        {r.label}
                      </p>
                      <Link href={r.href} className={buttonVariants({ size: "sm" })}>
                        {r.action}
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
              {todo && rows.length > 0 && (
                <p className="mt-2 text-right text-xs">
                  <Link href="/todo" className="text-muted-foreground hover:text-foreground">
                    查看全部待办 →
                  </Link>
                </p>
              )}
            </CardContent>
          </Card>
        </HideInCashierMode>
      </div>
    </main>
  )
}
