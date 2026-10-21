import Link from "next/link"

import { auth } from "@/auth"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorBanner } from "@/components/ui/error-banner"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import { formatCNY } from "@/lib/format"
import { normalizeRole, roleCan, type Perm } from "@/lib/roles"
import {
  fetchTodoSummary,
  type TodoBill,
  type TodoDebtor,
  type TodoNegativeStockItem,
  type TodoStockItem,
} from "@/lib/api/todo"

export const revalidate = 60

/**
 * /todo — "需要老板看一眼" 聚合。
 *
 * Five sections, each with the real total and a top-5 preview + one main button:
 *   1. Draft purchase bills awaiting review
 *   2. Draft sale bills awaiting review
 *   3. Low-stock products
 *   4. Customers owing for more than 30 days
 *   5. Approved sales not fully invoiced
 *
 * All numbers come from one backend call (GET /todo/summary). If the call, or
 * any source behind it, fails the page says so — it never shows an all-clear
 * it cannot vouch for.
 */
export default async function TodoPage() {
  const session = await auth()
  const accessToken = session?.accessToken ?? ""
  const summary = await fetchTodoSummary(accessToken)
  // 每个按钮通向需要权限的操作；角色未知时不遮挡，后端兜底。
  const role = normalizeRole(session?.user?.role, session?.user?.isOwner === true)
  const can = (p: Perm) => role === null || roleCan(role, p)

  if (!summary) {
    return (
      <PageContainer width="default">
        <PageHeader title="待办" subtitle="暂时无法获取待办事项" />
        <ErrorBanner hint="请稍后刷新页面再试">待办数据加载失败</ErrorBanner>
      </PageContainer>
    )
  }

  const partial = summary.failed.length > 0
  const isEmpty = summary.total === 0

  return (
    <PageContainer width="default">
      <PageHeader
        title="待办"
        subtitle={
          partial
            ? "部分数据加载失败，下面的数量可能不全"
            : isEmpty
              ? "暂时没有需要处理的事项"
              : `共 ${summary.total} 项需要你看一眼`
        }
      />

      {partial && (
        <ErrorBanner className="mb-4" hint="请稍后刷新页面再试">
          部分数据加载失败
        </ErrorBanner>
      )}

      {isEmpty ? (
        !partial && (
          <EmptyState
            title="一切都好"
            description="待审单据、低库存、欠款、待开票都已处理完；新的提醒会自动出现在这里。"
          />
        )
      ) : (
        <div className="space-y-6">
          <BillSection
            icon="🛒"
            title="待审采购单"
            count={summary.draft_purchases.count}
            items={summary.draft_purchases.items}
            allHref="/purchases?status=0&range=all"
            detailHref={(id) => `/purchases/${id}`}
            action={can("bill.approve") ? "去审核" : undefined}
            showAmount={can("cost.view")}
          />
          <BillSection
            icon="📊"
            title="待审销售单"
            count={summary.draft_sales.count}
            items={summary.draft_sales.items}
            allHref="/sales?status=0&range=all"
            detailHref={(id) => `/sales/${id}`}
            action={can("bill.approve") ? "去审核" : undefined}
          />
          <LowStockSection
            items={summary.low_stock.items}
            totalCount={summary.low_stock.count}
            canAct={can("bill.draft")}
            canList={can("replenish.view")}
          />
          <NegativeStockSection
            items={summary.negative_stock?.items}
            totalCount={summary.negative_stock?.count ?? 0}
          />
          <DebtorSection
            items={summary.overdue_customers.items}
            totalCount={summary.overdue_customers.count}
            canAct={can("payment.record")}
            showAmount={can("payment.read")}
          />
          <BillSection
            icon="🧾"
            title="还没开足发票的销售单"
            count={summary.uninvoiced_sales.count}
            items={summary.uninvoiced_sales.items}
            allHref="/sales?inv=none&range=all"
            detailHref={(id) => `/sales/${id}`}
            action={can("invoice.manage") ? "去开票" : undefined}
            showAmount={can("payment.read")}
          />
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
  href?: string
}) {
  return (
    <div className="mb-2 flex items-center justify-between">
      <h2 className="flex items-center gap-2 text-sm font-medium">
        <span aria-hidden="true">{icon}</span>
        {title}
        {count > 0 && <Badge tone="accent">{count}</Badge>}
      </h2>
      {href && (
        <Link href={href} className="text-xs text-muted-foreground hover:text-foreground">
          全部 →
        </Link>
      )}
    </div>
  )
}

function BillSection({
  icon,
  title,
  count,
  items,
  allHref,
  detailHref,
  action,
  showAmount = true,
}: {
  icon: string
  title: string
  count: number
  items: TodoBill[]
  allHref: string
  detailHref: (id: string) => string
  /** The button label; omitted when the viewer may not act on these bills. */
  action?: string
  /** False when the viewer may not see the money on these bills (purchase prices, receivables). */
  showAmount?: boolean
}) {
  if (count === 0) return null
  return (
    <section>
      <SectionHeader icon={icon} title={title} count={count} href={allHref} />
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((b) => (
          <li key={b.id} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{b.bill_no}</p>
              <p className="truncate text-xs text-muted-foreground">
                {b.partner_name || "散客"} · {b.bill_date}
              </p>
            </div>
            {showAmount && (
              <div className="text-right font-mono text-sm tabular-nums">{formatCNY(b.total_amount)}</div>
            )}
            {action && (
              <Link href={detailHref(b.id)} className={buttonVariants({ size: "sm" })}>
                {action}
              </Link>
            )}
          </li>
        ))}
      </ul>
      {count > items.length && (
        <p className="mt-1.5 text-center text-xs text-muted-foreground">
          还有 {count - items.length} 张，点右上角“全部”查看
        </p>
      )}
    </section>
  )
}

function LowStockSection({ items, totalCount, canAct, canList }: { items: TodoStockItem[]; totalCount: number; canAct: boolean; canList: boolean }) {
  if (totalCount === 0) return null
  return (
    <section>
      <SectionHeader icon="📉" title="低库存预警" count={totalCount} href={canList ? "/replenish" : undefined} />
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((i) => (
          <li key={i.product_id} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{i.product_name}</p>
              <p className="truncate text-xs text-muted-foreground">
                约 {Number(i.days_of_supply).toFixed(0)} 天可售
              </p>
            </div>
            <div className="text-right">
              <p className="font-mono text-sm tabular-nums text-destructive">
                {Number(i.available_qty).toFixed(0)}
                <span className="text-xs text-muted-foreground"> / {Number(i.reorder_point).toFixed(0)}</span>
              </p>
            </div>
            {canAct && (
              <Link
                href={`/purchases/new?prefill_product_id=${i.product_id}`}
                className={buttonVariants({ size: "sm" })}
              >
                去补货
              </Link>
            )}
          </li>
        ))}
      </ul>
      {totalCount > items.length && (
        <p className="mt-1.5 text-center text-xs text-muted-foreground">
          还有 {totalCount - items.length} 个商品{canList ? "，点右上角“全部”查看" : "，请联系老板或店长查看"}
        </p>
      )}
    </section>
  )
}

function NegativeStockSection({ items, totalCount }: { items?: TodoNegativeStockItem[]; totalCount: number }) {
  if (!items || totalCount === 0) return null
  return (
    <section>
      <SectionHeader icon="⚠️" title="负库存" count={totalCount} />
      <p className="mb-2 text-xs text-muted-foreground">离线销售导致负库存，请盘点</p>
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((i) => (
          <li key={`${i.product_id}-${i.warehouse_name}`} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{i.product_name}</p>
              <p className="truncate text-xs text-muted-foreground">{i.warehouse_name}</p>
            </div>
            <div className="text-right">
              <p className="font-mono text-sm tabular-nums text-destructive">{Number(i.on_hand).toFixed(0)}</p>
            </div>
          </li>
        ))}
      </ul>
      {totalCount > items.length && (
        <p className="mt-1.5 text-center text-xs text-muted-foreground">还有 {totalCount - items.length} 个商品</p>
      )}
    </section>
  )
}

function DebtorSection({
  items,
  totalCount,
  canAct,
  showAmount = true,
}: {
  items: TodoDebtor[]
  totalCount: number
  canAct: boolean
  showAmount?: boolean
}) {
  if (totalCount === 0) return null
  return (
    <section>
      <SectionHeader icon="💰" title="欠款超过 30 天的客户" count={totalCount} href="/customers" />
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((d, idx) => (
          <li key={d.partner_id ?? `walkin-${idx}`} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{d.partner_name || "散客"}</p>
              <p className="truncate text-xs text-muted-foreground">
                {d.oldest_bill_date ? `最早一笔欠款 ${d.oldest_bill_date}` : "超过 30 天未收"}
              </p>
            </div>
            {showAmount && (
              <div className="text-right font-mono text-sm tabular-nums text-destructive">
                {formatCNY(d.overdue_amount)}
              </div>
            )}
            {canAct && d.partner_id && (
              <Link
                href={`/sales?pay=open&range=all&partner=${d.partner_id}&pname=${encodeURIComponent(d.partner_name)}`}
                className={buttonVariants({ size: "sm" })}
              >
                去收款
              </Link>
            )}
          </li>
        ))}
      </ul>
      {totalCount > items.length && (
        <p className="mt-1.5 text-center text-xs text-muted-foreground">
          还有 {totalCount - items.length} 位客户，点右上角“全部”查看
        </p>
      )}
    </section>
  )
}
