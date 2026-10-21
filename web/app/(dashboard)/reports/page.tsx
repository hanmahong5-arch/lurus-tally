import { auth } from "@/auth"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { EmptyState } from "@/components/ui/empty-state"
import { formatCNY } from "@/lib/format"
import { normalizeRole, roleCan } from "@/lib/roles"
import { shanghaiToday } from "@/lib/date-range"
import { monthlyCsvLinks, previousMonth, resolveMonth, type MonthlyReport } from "@/lib/api/reports"
import {
  GrossMarginBlock,
  ABCBlock,
  DeadStockBlock,
  SalesTopBlock,
} from "./analytics-blocks"
import { ExchangeRateCard } from "./exchange-rate-card"

const SERVER_BACKEND_URL =
  typeof window === "undefined"
    ? (process.env.BACKEND_URL ?? "http://tally-backend:18200")
    : ""

/** Month-end figures from the backend; null when the request fails. */
async function fetchMonthlyReport(accessToken: string, month: string): Promise<MonthlyReport | null> {
  try {
    const res = await fetch(`${SERVER_BACKEND_URL}/api/v1/reports/monthly?month=${month}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: "no-store",
    })
    if (!res.ok) return null
    return (await res.json()) as MonthlyReport
  } catch {
    return null
  }
}

const FAILED = "数据加载失败"

const PILL = "rounded-full border px-3 py-1 text-sm"
const PILL_ON = `${PILL} border-primary bg-primary text-primary-foreground`
const PILL_OFF = `${PILL} border-border hover:bg-muted`

/** 账龄页链接带上月底，查上月时账龄合计才和卡片一致；本月还没过完就用今天。 */
function agingHref(base: string | undefined, dateTo: string | undefined, today: string) {
  if (!base || !dateTo) return base
  return `${base}&as_of=${dateTo > today ? today : dateTo}`
}

/**
 * /reports — 月底对账 + 经营分析 + CSV 导出.
 *
 * The month figures come from one backend call (net of returns, cut at Beijing
 * midnight), so the page and the CSV downloads agree. ?month=YYYY-MM picks the month.
 */
export default async function ReportsPage({ searchParams }: { searchParams?: { month?: string } }) {
  const session = await auth()
  const accessToken = session?.accessToken ?? ""
  // Cost and profit are for the owner, managers and finance. An unknown role
  // shows them (the backend trims the figures anyway); a known one without the
  // permission never sees them.
  const role = normalizeRole(session?.user?.role, session?.user?.isOwner === true)
  const showCost = role === null || roleCan(role, "cost.view")
  // 报表整页都要 report.view；没有的人不发任何报表请求，只看到说明。
  if (role !== null && !roleCan(role, "report.view")) {
    return (
      <PageContainer width="default">
        <PageHeader title="报表" subtitle="月底对账 · 经营分析 · 数据导出" />
        <EmptyState title="报表只对老板、店长和财务开放" description="需要查看报表时，请联系老板或店长。" />
      </PageContainer>
    )
  }

  const today = shanghaiToday()
  const current = today.slice(0, 7)
  const last = previousMonth(current)
  const month = resolveMonth(searchParams?.month, current)
  const report = accessToken ? await fetchMonthlyReport(accessToken, month) : null
  const links = report ? monthlyCsvLinks(report) : null
  const [y, m] = month.split("-")
  const val = (v: string | undefined) => (report && v !== undefined ? formatCNY(v) : FAILED)

  return (
    <PageContainer width="default">
      <PageHeader title="报表" subtitle={`${y} 年 ${Number(m)} 月对账 · 经营分析 · 数据导出`} />

      <div className="space-y-6">
        <section className="flex flex-wrap items-center gap-2" aria-label="选择月份">
          <a href={`?month=${current}`} className={month === current ? PILL_ON : PILL_OFF}>
            本月
          </a>
          <a href={`?month=${last}`} className={month === last ? PILL_ON : PILL_OFF}>
            上月
          </a>
          <form method="get" className="flex items-center gap-2">
            <label htmlFor="report-month" className="text-sm text-muted-foreground">
              其他月份
            </label>
            <input
              id="report-month"
              type="month"
              name="month"
              defaultValue={month}
              max={current}
              className="h-8 rounded-lg border border-input bg-transparent px-2 text-sm"
            />
            <button type="submit" className="rounded-md border border-border bg-background px-3 py-1 text-sm hover:bg-muted/50">
              查看
            </button>
          </form>
        </section>

        <section>
          <h2 className="mb-3 text-sm font-medium">月底对账</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <StatCard label="净销售额" value={val(report?.net_sales)} hint="销售减去退货" href={links?.bills} />
            {showCost && (
              <>
                <StatCard label="销售成本" value={val(report?.cost_of_goods)} hint="卖出商品的进货成本，退货已扣回" />
                <StatCard label="毛利" value={val(report?.gross_profit)} hint="净销售额减销售成本" href={links?.summary} />
              </>
            )}
            <StatCard label="采购净额" value={val(report?.net_purchases)} hint="采购减去采购退货" href={links?.bills} />
            <StatCard label="本月收款" value={val(report?.receipts)} hint="实际收到的钱，已扣退款；赊账不算" href={links?.receipts} />
            <StatCard label="本月付款" value={val(report?.payments)} hint="实际付出的钱，已扣供应商退款；退货冲抵不算" href={links?.paymentsOut} />
            <StatCard label="月底应收" value={val(report?.receivable)} hint="当月底前开的单，扣掉到今天为止的收款后还欠的" href={agingHref(links?.receivable, report?.date_to, today)} linkLabel="查看账龄" />
            <StatCard label="月底应付" value={val(report?.payable)} hint="和「应付账龄」合计一致" href={agingHref(links?.payable, report?.date_to, today)} linkLabel="查看账龄" />
            {showCost && (
              <StatCard label="库存金额" value={val(report?.inventory_value)} hint="现在的库存数量 × 成本价，不随月份变" href={links?.stock} />
            )}
          </div>
        </section>

        <section>
          <h2 className="mb-3 text-sm font-medium">数据导出</h2>
          <div className="grid gap-3 sm:grid-cols-4">
            <ExportCard
              title="单据表格"
              description={report ? `${report.month} 的销售、采购和退货单据` : "所有销售 / 采购 / 退货单据"}
              href={links?.bills ?? "/api/proxy/exports/bills.csv"}
            />
            <ExportCard
              title="库存表格"
              description={showCost ? "当前库存、成本价和库存金额" : "当前库存数量"}
              href="/api/proxy/exports/stock.csv"
            />
            <ExportCard
              title="收付款表格"
              description={report ? `${report.month} 的收付款流水` : "所有收付款流水"}
              href={links?.payments ?? "/api/proxy/exports/payments.csv"}
            />
            <ExportCard
              title="对账汇总表格"
              description="上面这些数字，交给会计用"
              href={links?.summary ?? `/api/proxy/reports/monthly.csv?month=${month}`}
            />
          </div>
        </section>

        <section className="space-y-3">
          <h2 className="text-sm font-medium">经营分析</h2>
          <p className="text-xs text-muted-foreground">每块都可以导出表格。</p>
          <div className="grid gap-4 sm:grid-cols-2">
            {showCost && <GrossMarginBlock />}
            <ABCBlock />
            <DeadStockBlock />
            <SalesTopBlock />
          </div>
        </section>

        <ExchangeRateCard />
      </div>
    </PageContainer>
  )
}

function StatCard({
  label,
  value,
  hint,
  href,
  linkLabel,
}: {
  label: string
  value: string
  hint: string
  href?: string
  /** Set for a link to another page; without it the link downloads a file. */
  linkLabel?: string
}) {
  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-xl tabular-nums">{value}</div>
      <div className="mt-1 text-[10px] text-muted-foreground/70">{hint}</div>
      {href && (
        <a
          href={href}
          download={linkLabel ? undefined : true}
          className="mt-2 inline-block text-xs text-primary hover:underline"
        >
          {linkLabel ?? "下载明细"}
        </a>
      )}
    </div>
  )
}

function ExportCard({
  title,
  description,
  href,
}: {
  title: string
  description: string
  href: string
}) {
  return (
    <a
      href={href}
      download
      className="block rounded-xl border border-border bg-card p-4 transition-colors hover:bg-muted/50"
    >
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-medium">{title}</div>
          <div className="mt-1 text-xs text-muted-foreground">{description}</div>
        </div>
        <span className="rounded-md border border-border bg-background px-2 py-1 text-xs">↓</span>
      </div>
    </a>
  )
}
