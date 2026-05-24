import Link from "next/link"

import { auth } from "@/auth"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { formatCNY } from "@/lib/format"
import {
  GrossMarginBlock,
  ABCBlock,
  DeadStockBlock,
  SalesTopBlock,
} from "./analytics-blocks"

export const revalidate = 300

const SERVER_BACKEND_URL =
  typeof window === "undefined"
    ? (process.env.BACKEND_URL ?? "http://tally-backend:18200")
    : ""

interface ListBillsResp<T> {
  items: T[]
  total: number
}

interface BillSummary {
  total_amount: string
  bill_date: string
  status: number
}

function monthStartIso(): string {
  const now = new Date()
  return new Date(now.getFullYear(), now.getMonth(), 1).toISOString().slice(0, 10)
}

async function fetchMTDBills(
  accessToken: string,
  kind: "sale-bills" | "purchase-bills",
): Promise<BillSummary[]> {
  // The backend doesn't filter by date directly; pull approved status and
  // accept that callers might see slightly more than this month. Aggregation
  // below filters client-side to MTD.
  const res = await fetch(
    `${SERVER_BACKEND_URL}/api/v1/${kind}?status=2&size=200`,
    {
      headers: { Authorization: `Bearer ${accessToken}` },
      next: { revalidate: 300 },
    },
  )
  if (!res.ok) return []
  const body = (await res.json()) as ListBillsResp<BillSummary>
  return body.items ?? []
}

function sumMTD(bills: BillSummary[]): number {
  const start = monthStartIso()
  let sum = 0
  for (const b of bills) {
    if (!b.bill_date || b.bill_date.slice(0, 10) < start) continue
    const n = Number(b.total_amount)
    if (Number.isFinite(n)) sum += n
  }
  return sum
}

/**
 * /reports — 经营报表 + CSV 导出 + 汇率历史入口.
 *
 * Stats (本月销售额 / 采购额 / 单据数) come from listing approved bills and
 * filtering client-side. CSV exports hit /api/v1/exports/* directly (same
 * pattern the purchases / stock pages already use).
 *
 * 汇率历史 lives under /finance/exchange-rates today; the sidebar entry has
 * been removed in Phase 1 and this page is the new doorway.
 */
export default async function ReportsPage() {
  const session = await auth()
  const accessToken = session?.accessToken ?? ""

  const [salesBills, purchaseBills] = await Promise.all([
    accessToken ? fetchMTDBills(accessToken, "sale-bills") : Promise.resolve([]),
    accessToken ? fetchMTDBills(accessToken, "purchase-bills") : Promise.resolve([]),
  ])

  const salesMTD = sumMTD(salesBills)
  const purchaseMTD = sumMTD(purchaseBills)
  const start = monthStartIso()
  const salesCount = salesBills.filter(
    (b) => b.bill_date?.slice(0, 10) >= start,
  ).length
  const purchaseCount = purchaseBills.filter(
    (b) => b.bill_date?.slice(0, 10) >= start,
  ).length

  return (
    <PageContainer width="default">
      <PageHeader
        title="报表"
        subtitle={`本月经营概览 · CSV 导出 · 汇率历史。统计区间为本自然月（${start} 起）。`}
      />

      <div className="space-y-6">
      <section className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <StatCard label="本月销售额" value={formatCNY(salesMTD)} hint="已审核销售单合计" />
        <StatCard label="本月采购额" value={formatCNY(purchaseMTD)} hint="已审核采购单合计" />
        <StatCard label="本月销售单数" value={String(salesCount)} hint="status=approved" />
        <StatCard label="本月采购单数" value={String(purchaseCount)} hint="status=approved" />
      </section>

      <section>
        <h2 className="mb-3 text-sm font-medium">数据导出</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <ExportCard
            title="单据 CSV"
            description="所有销售 / 采购单据明细"
            href="/api/v1/exports/bills.csv"
          />
          <ExportCard
            title="库存 CSV"
            description="当前库存快照（含成本均价）"
            href="/api/v1/exports/stock.csv"
          />
          <ExportCard
            title="付款 CSV"
            description="所有付款流水"
            href="/api/v1/exports/payments.csv"
          />
        </div>
      </section>

      {/* ── AI Analytics — decision-oriented blocks ────────────────────── */}
      <section className="space-y-3">
        <h2 className="text-sm font-medium">AI 分析决策</h2>
        <p className="text-xs text-muted-foreground">
          每块均可导出 CSV，点击「→ 建议」按钮可直接唤起 AI 助手并预填查询。
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          <GrossMarginBlock />
          <ABCBlock />
          <DeadStockBlock />
          <SalesTopBlock />
        </div>
      </section>

      <section>
        <h2 className="mb-3 text-sm font-medium">其他报表</h2>
        <Link href="/finance/exchange-rates">
          <Card className="transition-colors hover:bg-muted/50">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <span aria-hidden="true">💱</span>
                汇率历史
              </CardTitle>
              <CardDescription>
                跨境结汇所需的美元 / 欧元 / 港币历史曲线，含手动录入入口。
              </CardDescription>
            </CardHeader>
            <CardContent />
          </Card>
        </Link>
      </section>
      </div>
    </PageContainer>
  )
}

function StatCard({
  label,
  value,
  hint,
}: {
  label: string
  value: string
  hint: string
}) {
  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-xl tabular-nums">{value}</div>
      <div className="mt-1 text-[10px] text-muted-foreground/70">{hint}</div>
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
