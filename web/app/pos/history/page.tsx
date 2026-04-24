"use client"

import { useState, useEffect } from "react"
import Link from "next/link"
import Decimal from "decimal.js"
import { listTodaySaleBills, type SaleBillSummary } from "@/lib/api/pos"

// Story 2.1 TODO: replace with session tenantId once auth wired
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const PAYMENT_METHOD_LABELS: Record<string, { label: string; className: string }> = {
  cash: { label: "现金", className: "bg-emerald-100 text-emerald-700" },
  wechat: { label: "微信", className: "bg-green-100 text-green-700" },
  alipay: { label: "支付宝", className: "bg-blue-100 text-blue-700" },
  card: { label: "刷卡", className: "bg-purple-100 text-purple-700" },
  credit: { label: "赊账", className: "bg-orange-100 text-orange-700" },
  transfer: { label: "转账", className: "bg-cyan-100 text-cyan-700" },
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
 * POS history page — shows today's completed sale bills with summary statistics.
 */
export default function PosHistoryPage() {
  const [bills, setBills] = useState<SaleBillSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    listTodaySaleBills(devTenantId)
      .then(setBills)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  // Compute summary stats
  const totalCount = bills.length
  const totalAmount = bills.reduce(
    (acc, b) => acc.plus(new Decimal(b.total_amount || "0")),
    new Decimal(0)
  )

  // Group by payment method
  const byMethod: Record<string, Decimal> = {}
  for (const b of bills) {
    const m = b.payment_method
    byMethod[m] = (byMethod[m] ?? new Decimal(0)).plus(new Decimal(b.paid_amount || "0"))
  }

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
          <h1 className="text-base font-semibold">今日结单记录</h1>
        </div>
      </header>

      <div className="mx-auto max-w-3xl p-4">
        {/* Summary stats */}
        <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div className="rounded-xl border border-border bg-background p-4 text-center">
            <div className="text-3xl font-bold tabular-nums">{totalCount}</div>
            <div className="mt-1 text-xs text-muted-foreground">今日笔数</div>
          </div>
          <div className="rounded-xl border border-border bg-background p-4 text-center">
            <div className="text-2xl font-bold tabular-nums text-emerald-600">
              ¥{totalAmount.toFixed(2)}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">今日总额</div>
          </div>
          {Object.entries(byMethod).map(([method, amount]) => {
            const meta = PAYMENT_METHOD_LABELS[method] ?? { label: method, className: "bg-muted text-muted-foreground" }
            return (
              <div key={method} className="rounded-xl border border-border bg-background p-4 text-center">
                <div className="text-xl font-bold tabular-nums">¥{amount.toFixed(2)}</div>
                <div className="mt-1">
                  <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${meta.className}`}>
                    {meta.label}
                  </span>
                </div>
              </div>
            )
          })}
        </div>

        {/* Bill list */}
        {loading && (
          <div className="py-12 text-center text-sm text-muted-foreground">加载中...</div>
        )}

        {error && (
          <div className="rounded-lg bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {!loading && !error && bills.length === 0 && (
          <div className="py-16 text-center text-muted-foreground">今日暂无交易记录</div>
        )}

        {!loading && bills.length > 0 && (
          <div className="overflow-hidden rounded-xl border border-border bg-background">
            <table className="w-full text-sm">
              <thead className="border-b border-border bg-muted/50 text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">单号</th>
                  <th className="px-4 py-2.5 text-left font-medium">时间</th>
                  <th className="px-4 py-2.5 text-right font-medium">金额</th>
                  <th className="px-4 py-2.5 text-right font-medium">应收余额</th>
                  <th className="px-4 py-2.5 text-center font-medium">收款方式</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {bills.map((bill) => {
                  const receivable = new Decimal(bill.total_amount || "0").minus(
                    new Decimal(bill.paid_amount || "0")
                  )
                  const meta = PAYMENT_METHOD_LABELS[bill.payment_method] ?? {
                    label: bill.payment_method,
                    className: "bg-muted text-muted-foreground",
                  }
                  return (
                    <tr key={bill.id} className="hover:bg-muted/30 transition-colors">
                      <td className="px-4 py-2.5 font-mono text-xs font-medium">
                        {bill.bill_no}
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground">
                        {formatTime(bill.created_at)}
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums font-medium">
                        ¥{bill.total_amount}
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums">
                        {receivable.gt(0) ? (
                          <span className="text-orange-600">¥{receivable.toFixed(2)}</span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-center">
                        <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${meta.className}`}>
                          {meta.label}
                        </span>
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
