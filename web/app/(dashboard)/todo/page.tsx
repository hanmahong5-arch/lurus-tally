import Link from "next/link"

import { auth } from "@/auth"
import { EmptyState } from "@/components/ui/empty-state"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Badge } from "@/components/ui/badge"
import {
  fetchDraftPurchaseBillCount,
  fetchLowStockAlerts,
  type LowStockItem,
} from "@/lib/api/stock"

export const revalidate = 60

const SERVER_BACKEND_URL =
  typeof window === "undefined"
    ? (process.env.BACKEND_URL ?? "http://tally-backend:18200")
    : ""

interface DraftSaleHead {
  id: string
  bill_no: string
  partner_name?: string
  total_amount: string
  created_at: string
}

interface DraftPurchaseHead {
  id: string
  bill_no: string
  partner_name?: string
  total_amount: string
  created_at: string
}

interface ListBillsResp<T> {
  items: T[]
  total: number
}

async function fetchDraftSaleBills(accessToken: string): Promise<DraftSaleHead[]> {
  const res = await fetch(`${SERVER_BACKEND_URL}/api/v1/sale-bills?status=0&size=5`, {
    headers: { Authorization: `Bearer ${accessToken}` },
    next: { revalidate: 60 },
  })
  if (!res.ok) return []
  const body = (await res.json()) as ListBillsResp<DraftSaleHead>
  return body.items ?? []
}

async function fetchDraftPurchaseBills(accessToken: string): Promise<DraftPurchaseHead[]> {
  const res = await fetch(`${SERVER_BACKEND_URL}/api/v1/purchase-bills?status=0&size=5`, {
    headers: { Authorization: `Bearer ${accessToken}` },
    next: { revalidate: 60 },
  })
  if (!res.ok) return []
  const body = (await res.json()) as ListBillsResp<DraftPurchaseHead>
  return body.items ?? []
}

/**
 * /todo — "需要老板看一眼" 聚合。
 *
 * Three sections, each with a top-5 preview + jump-to-source link:
 *   1. Low-stock SKUs (server-side from /stock/alerts/low-stock)
 *   2. Draft purchase bills awaiting approval
 *   3. Draft sale bills awaiting approval
 *
 * Fully server-rendered with `revalidate: 60` so the badge count and list
 * never drift more than a minute behind reality.
 */
export default async function TodoPage() {
  const session = await auth()
  const accessToken = session?.accessToken ?? ""

  const [lowStock, draftPurchases, draftSales, draftPurchaseTotal] = await Promise.all([
    accessToken ? fetchLowStockAlerts(accessToken, 5) : Promise.resolve({ items: [], count: 0 }),
    accessToken ? fetchDraftPurchaseBills(accessToken) : Promise.resolve([]),
    accessToken ? fetchDraftSaleBills(accessToken) : Promise.resolve([]),
    accessToken ? fetchDraftPurchaseBillCount(accessToken) : Promise.resolve(0),
  ])

  const totalCount = lowStock.count + draftPurchaseTotal + draftSales.length
  const isEmpty = totalCount === 0

  return (
    <PageContainer width="default">
      <PageHeader
        title="待办"
        subtitle={isEmpty ? "无待处理事项 — 今日通关 🎉" : `共 ${totalCount} 项需要你看一眼`}
      />

      {isEmpty ? (
        <EmptyState
          title="一切都好"
          description="低库存、草稿单据、待审项都已清零；新提醒会自动出现在这里。"
        />
      ) : (
        <div className="space-y-6">
          <LowStockSection items={lowStock.items} totalCount={lowStock.count} />
          <DraftPurchaseSection items={draftPurchases} totalCount={draftPurchaseTotal} />
          <DraftSaleSection items={draftSales} />
        </div>
      )}
    </PageContainer>
  )
}

function SectionHeader({
  icon,
  title,
  count,
  href,
}: {
  icon: string
  title: string
  count: number
  href: string
}) {
  return (
    <div className="mb-2 flex items-center justify-between">
      <h2 className="flex items-center gap-2 text-sm font-medium">
        <span aria-hidden="true">{icon}</span>
        {title}
        {count > 0 && <Badge tone="accent">{count}</Badge>}
      </h2>
      <Link href={href} className="text-xs text-muted-foreground hover:text-foreground">
        全部 →
      </Link>
    </div>
  )
}

function LowStockSection({ items, totalCount }: { items: LowStockItem[]; totalCount: number }) {
  if (totalCount === 0) return null
  return (
    <section>
      <SectionHeader icon="📉" title="低库存预警" count={totalCount} href="/stock" />
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((i) => (
          <li
            key={`${i.product_id}-${i.warehouse_id}`}
            className="flex items-center justify-between gap-3 px-4 py-3"
          >
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{i.product_name}</p>
              <p className="truncate text-xs text-muted-foreground">{i.warehouse_name}</p>
            </div>
            <div className="text-right">
              <p className="font-mono text-sm tabular-nums text-destructive">
                {Number(i.available_qty).toFixed(0)}
                <span className="text-xs text-muted-foreground">
                  {" "}
                  / {Number(i.low_safe_qty).toFixed(0)}
                </span>
              </p>
              <Link
                href={`/purchases/new?prefill_product_id=${i.product_id}`}
                className="text-xs text-primary hover:underline"
              >
                下采购单
              </Link>
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}

function DraftPurchaseSection({
  items,
  totalCount,
}: {
  items: DraftPurchaseHead[]
  totalCount: number
}) {
  if (totalCount === 0) return null
  return (
    <section>
      <SectionHeader
        icon="🛒"
        title="待审采购单"
        count={totalCount}
        href="/purchases?status=draft"
      />
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((b) => (
          <li key={b.id} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <Link
                href={`/purchases/${b.id}`}
                className="text-sm font-medium hover:underline"
              >
                {b.bill_no}
              </Link>
              <p className="truncate text-xs text-muted-foreground">{b.partner_name ?? "—"}</p>
            </div>
            <div className="text-right text-sm font-mono tabular-nums">
              {b.total_amount}
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}

function DraftSaleSection({ items }: { items: DraftSaleHead[] }) {
  if (items.length === 0) return null
  return (
    <section>
      <SectionHeader icon="📊" title="待审销售单" count={items.length} href="/sales?status=draft" />
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((b) => (
          <li key={b.id} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <Link href={`/sales/${b.id}`} className="text-sm font-medium hover:underline">
                {b.bill_no}
              </Link>
              <p className="truncate text-xs text-muted-foreground">{b.partner_name ?? "—"}</p>
            </div>
            <div className="text-right text-sm font-mono tabular-nums">{b.total_amount}</div>
          </li>
        ))}
      </ul>
    </section>
  )
}
