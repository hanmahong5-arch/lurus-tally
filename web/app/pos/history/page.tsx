"use client"

import { useState } from "react"
import Link from "next/link"
import Decimal from "decimal.js"
import { listTodaySaleBills, getPosSummary, type SaleBillSummary, type PosSummary, type PosCashier } from "@/lib/api/pos"
import { formatCNY } from "@/lib/format"
import { ErrorBanner } from "@/components/ui/error-banner"
import { extractApiError } from "@/lib/api/errors"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { CloudPrintButton } from "@/components/print/cloud-print-button"
import { PendingCheckouts } from "@/components/pos/pending-checkouts"
import { useCheckoutQueue } from "@/hooks/use-checkout-queue"
import { unsentCount } from "@/lib/pos/checkout-queue"

const PAYMENT_METHOD_LABELS: Record<string, { label: string; className: string }> = {
  cash: { label: "现金", className: "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300" },
  wechat: { label: "微信", className: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300" },
  alipay: { label: "支付宝", className: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300" },
  card: { label: "银行卡", className: "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300" },
  credit: { label: "赊账", className: "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300" },
  transfer: { label: "转账", className: "bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300" },
}

const PAID_STATUS = {
  paid: { label: "已收", className: "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300" },
  credit: { label: "赊账", className: "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300" },
}

function formatTime(isoString: string): string {
  try {
    const d = new Date(isoString)
    return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" })
  } catch {
    return isoString
  }
}

/**
 * POS history page — shift-end view of today's (Beijing time) sales. The figures come from
 * the server summary so they cover every bill of the day; the table lists the latest ones.
 */
export default function PosHistoryPage() {
  const [bills, setBills] = useState<SaleBillSummary[]>([])
  const [summary, setSummary] = useState<PosSummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [onlyMine, setOnlyMine] = useState(false)
  // The cashier whose shift is open (owner / manager only), and the shop-wide roster to pick from.
  const [picked, setPicked] = useState<PosCashier | null>(null)
  const [roster, setRoster] = useState<PosCashier[]>([])
  const tenantId = useTenantId()
  // 本机台账：接口不通时，今日结单退化为只列本机还没上传的单。
  const queue = useCheckoutQueue()
  const localUnsent = unsentCount(queue.entries)
  // A cashier closes their own shift; the owner and manager can flip to the whole shop.
  // The session arrives with the page, so the first request already knows; an unknown role is treated as a cashier.
  const { can } = useRole()
  const cashierOnly = !can("bill.return")
  const mine = cashierOnly || onlyMine
  const pickedKey = !mine && !cashierOnly ? (picked?.key ?? null) : null

  useAbortableEffect((signal, isCancelled) => {
    setLoading(true)
    setError(null)
    // The bill list has no per-person filter, so a picked cashier's shift shows figures only.
    const req = pickedKey
      ? Promise.all([getPosSummary(undefined, tenantId, signal, false, pickedKey), Promise.resolve<SaleBillSummary[]>([])])
      : Promise.all([getPosSummary(undefined, tenantId, signal, mine), listTodaySaleBills(tenantId, signal, mine)])
    req
      .then(([sum, data]) => {
        if (isCancelled()) return
        setSummary(sum)
        setBills(data)
        if (!mine && !pickedKey) setRoster(sum.cashiers ?? [])
      })
      .catch((e) => {
        if (isCancelled() || signal.aborted) return
        setError(extractApiError(e).message)
      })
      .finally(() => {
        if (isCancelled()) return
        setLoading(false)
      })
  }, [tenantId, mine, pickedKey])

  const byMethod = summary?.by_pay_type ?? []
  const credit = new Decimal(summary?.credit_amount || "0")
  const noSales = !!summary && summary.count === 0 && summary.return_count === 0 && summary.refund_count === 0 && bills.length === 0

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-900">
      {/* Top bar */}
      <header className="sticky top-0 z-10 border-b border-border bg-background px-4 py-3">
        <div className="flex items-center gap-4">
          <Link
            href="/pos"
            className="text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            ← 返回收银台
          </Link>
          <h1 className="text-base font-semibold">
            {cashierOnly ? "我的今日结单" : pickedKey && picked ? `${picked.name}的今日结单` : "今日结单"}
          </h1>
          {pickedKey && (
            <button
              type="button"
              onClick={() => setPicked(null)}
              className="text-sm text-primary underline underline-offset-2"
            >
              返回全店
            </button>
          )}
          {!cashierOnly && (
            <div className="ml-auto flex overflow-hidden rounded-lg border border-border text-xs" role="group" aria-label="结单范围">
              <button
                type="button"
                aria-pressed={!onlyMine}
                onClick={() => { setOnlyMine(false); setPicked(null) }}
                className={`px-3 py-1.5 ${!onlyMine ? "bg-primary text-primary-foreground" : "text-muted-foreground"}`}
              >
                全店
              </button>
              <button
                type="button"
                aria-pressed={onlyMine}
                onClick={() => { setOnlyMine(true); setPicked(null) }}
                className={`px-3 py-1.5 ${onlyMine ? "bg-primary text-primary-foreground" : "text-muted-foreground"}`}
              >
                只看我的
              </button>
            </div>
          )}
        </div>
      </header>

      <div className="mx-auto max-w-3xl p-4">
        {mine && <p className="mb-3 text-xs text-muted-foreground">
            {can("report.view") ? "只算你本人，全店请看报表。" : "只算你本人，全店请问老板或店长。"}
          </p>}
        {/* Summary stats */}
        {summary && (
          <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div className="rounded-xl border border-border bg-background p-4 text-center">
              <div className="text-3xl font-bold tabular-nums">{summary.count}</div>
              <div className="mt-1 text-xs text-muted-foreground">今日笔数</div>
            </div>
            <div className="rounded-xl border border-border bg-background p-4 text-center">
              <div className="text-2xl font-bold tabular-nums text-success">
                {formatCNY(summary.total_amount)}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">今日总额</div>
            </div>
            {byMethod.map((row) => {
              const meta = PAYMENT_METHOD_LABELS[row.pay_type] ?? { label: "其他方式", className: "bg-muted text-muted-foreground" }
              const refundOnly = row.count === 0 && new Decimal(row.amount || "0").lt(0)
              return (
                <div key={row.pay_type} className="rounded-xl border border-border bg-background p-4 text-center">
                  <div className="text-xl font-bold tabular-nums">{formatCNY(row.amount)}</div>
                  <div className="mt-1">
                    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${meta.className}`}>
                      {refundOnly ? `${meta.label} · 代退退款` : meta.label}
                    </span>
                    {!refundOnly && <span className="ml-1 text-xs text-muted-foreground">{row.count} 笔</span>}
                  </div>
                </div>
              )
            })}
            {credit.gt(0) && (
              <div className="rounded-xl border border-border bg-background p-4 text-center">
                <div className="text-xl font-bold tabular-nums text-warning">{formatCNY(credit.toNumber())}</div>
                <div className="mt-1 text-xs text-muted-foreground">今日未收款</div>
              </div>
            )}
            {summary.return_count > 0 && (
              <div className="rounded-xl border border-border bg-background p-4 text-center">
                <div className="text-xl font-bold tabular-nums text-destructive">
                  -{formatCNY(summary.return_amount)}
                </div>
                <div className="mt-1 text-xs text-muted-foreground">退货 {summary.return_count} 笔</div>
              </div>
            )}
            {summary.refund_count > 0 && (
              <div className="rounded-xl border border-border bg-background p-4 text-center">
                <div className="text-xl font-bold tabular-nums text-destructive">
                  -{formatCNY(summary.refund_amount)}
                </div>
                <div className="mt-1 text-xs text-muted-foreground">退款 {summary.refund_count} 笔</div>
              </div>
            )}
          </div>
        )}

        {/* Per-cashier split: owner and manager, shop-wide view, when more than one person took money */}
        {!mine && !pickedKey && !loading && roster.length > 1 && (
          <div className="mb-4 rounded-xl border border-border bg-background">
            <div className="border-b border-border px-4 py-2.5 text-sm font-medium">按收银员</div>
            <ul className="divide-y divide-border">
              {roster.map((r) => (
                <li key={r.key}>
                  <button
                    type="button"
                    onClick={() => setPicked(r)}
                    className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-muted/30"
                  >
                    <span className="flex-1 font-medium">{r.name}</span>
                    <span className="text-muted-foreground">{r.count} 单</span>
                    <span className="tabular-nums">实收 {formatCNY(r.paid_amount)}</span>
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* Bill list */}
        {loading && (
          <div className="py-12 text-center text-sm text-muted-foreground">加载中...</div>
        )}

        {error && (
          <>
            <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>
            <section className="mt-3" aria-label="离线视图">
              <p className="mb-2 text-sm font-medium">离线，仅显示本机未上传的 {localUnsent} 笔</p>
              {queue.entries.length > 0 && (
                <PendingCheckouts inline entries={queue.entries} onRetry={queue.retry} onDiscard={queue.discard} />
              )}
            </section>
          </>
        )}

        {!loading && !error && noSales && (
          <div className="py-16 text-center text-muted-foreground">今日暂无交易记录</div>
        )}

        {!loading && bills.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-border bg-background">
            <table className="w-full text-sm">
              <thead className="border-b border-border bg-muted/50 text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">单号</th>
                  <th className="px-4 py-2.5 text-left font-medium">时间</th>
                  <th className="px-4 py-2.5 text-right font-medium">金额</th>
                  <th className="px-4 py-2.5 text-right font-medium">未收</th>
                  <th className="px-4 py-2.5 text-center font-medium">收款情况</th>
                  <th className="px-4 py-2.5 text-center font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {bills.map((bill) => {
                  // unpaid_amount is already net of returns and never negative.
                  const receivable = new Decimal(bill.unpaid_amount || "0")
                  const meta = receivable.gt(0) ? PAID_STATUS.credit : PAID_STATUS.paid
                  return (
                    <tr key={bill.id} className="hover:bg-muted/30 transition-colors">
                      <td className="px-4 py-2.5 font-mono text-xs font-medium">
                        {bill.bill_no}
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground">
                        {formatTime(bill.created_at)}
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums font-medium">
                        {formatCNY(bill.total_amount)}
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums">
                        {receivable.gt(0) ? (
                          <span className="text-warning">{formatCNY(receivable.toNumber())}</span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-center">
                        <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${meta.className}`}>
                          {meta.label}
                        </span>
                      </td>
                      <td className="px-4 py-2.5 text-center">
                        <a
                          href={`/print/pos/${bill.id}`}
                          target="_blank"
                          rel="noreferrer"
                          className="text-xs text-primary underline underline-offset-2"
                          aria-label={`补打小票 ${bill.bill_no}`}
                        >
                          补打小票
                        </a>
                        <CloudPrintButton billId={bill.id} className="ml-2 h-7 px-2 text-xs" />

                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
