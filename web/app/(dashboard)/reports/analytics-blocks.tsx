"use client"

import { useEffect, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import {
  fetchGrossMargin,
  fetchABC,
  fetchDeadStock,
  fetchSalesTop,
  downloadCSV,
  type GrossMarginResult,
  type ABCResult,
  type DeadStockResult,
  type SalesTopResult,
  type SalesMetric,
} from "@/lib/api/reports"

// ── Gross Margin Block ────────────────────────────────────────────────────────

export function GrossMarginBlock() {
  const [days, setDays] = useState(30)
  const [data, setData] = useState<GrossMarginResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    setError(null)
    fetchGrossMargin(days, ac.signal)
      .then(setData)
      .catch((e: Error) => { if (e.name !== "AbortError") setError(e.message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [days])

  function handleExport() {
    if (!data) return
    const rows = [
      ...data.top10.map((p) => ({ tier: "top10", name: p.name, avg_margin: p.avg_margin })),
      ...data.bottom10.map((p) => ({ tier: "bottom10", name: p.name, avg_margin: p.avg_margin })),
    ]
    downloadCSV(rows, `gross-margin-${days}d.csv`)
  }

  function fireCTA() {
    window.dispatchEvent(
      new CustomEvent("tally:ai-query", {
        detail: { query: `分析低毛利商品并建议调价，参考最近 ${days} 天的毛利数据` },
      }),
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-2 pb-2">
        <div>
          <CardTitle className="text-base">毛利汇总</CardTitle>
          <CardDescription>整体毛利率 + 高/低毛利 TOP10</CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs"
          >
            {[7, 14, 30, 90].map((d) => (
              <option key={d} value={d}>{d}天</option>
            ))}
          </select>
          <button
            onClick={handleExport}
            disabled={!data}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs hover:bg-muted/50 disabled:opacity-40"
          >
            ↓ CSV
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {loading && <p className="py-4 text-center text-sm text-muted-foreground">加载中…</p>}
        {error && <p className="py-4 text-center text-sm text-red-500">{error}</p>}
        {data && !loading && (
          <div className="space-y-3">
            <div className="text-2xl font-bold tabular-nums">{data.overall_margin}</div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <p className="mb-1 text-xs font-medium text-muted-foreground">毛利最高</p>
                <ul className="space-y-0.5">
                  {data.top10.slice(0, 5).map((p) => (
                    <li key={p.name} className="flex justify-between text-xs">
                      <span className="truncate">{p.name}</span>
                      <span className="ml-2 tabular-nums text-green-600">{p.avg_margin}</span>
                    </li>
                  ))}
                </ul>
              </div>
              <div>
                <p className="mb-1 text-xs font-medium text-muted-foreground">毛利最低</p>
                <ul className="space-y-0.5">
                  {data.bottom10.slice(0, 5).map((p) => (
                    <li key={p.name} className="flex justify-between text-xs">
                      <span className="truncate">{p.name}</span>
                      <span className="ml-2 tabular-nums text-red-500">{p.avg_margin}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
            <button
              onClick={fireCTA}
              className="mt-2 w-full rounded-md bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 transition-colors"
            >
              建议调价 →
            </button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ── ABC Classification Block ──────────────────────────────────────────────────

const TIER_COLORS: Record<string, string> = {
  a: "bg-green-500",
  b: "bg-yellow-400",
  c: "bg-red-400",
}

export function ABCBlock() {
  const [data, setData] = useState<ABCResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    setError(null)
    fetchABC(ac.signal)
      .then(setData)
      .catch((e: Error) => { if (e.name !== "AbortError") setError(e.message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [])

  function handleExport() {
    if (!data) return
    downloadCSV(
      [
        { tier: "A", sku_count: data.a.sku_count, revenue_share: data.a.revenue_share },
        { tier: "B", sku_count: data.b.sku_count, revenue_share: data.b.revenue_share },
        { tier: "C", sku_count: data.c.sku_count, revenue_share: data.c.revenue_share },
      ],
      "abc-classification.csv",
    )
  }

  function fireCTA() {
    window.dispatchEvent(
      new CustomEvent("tally:ai-query", {
        detail: { query: "查看 A 类商品库存状态，判断是否需要补货" },
      }),
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-2 pb-2">
        <div>
          <CardTitle className="text-base">ABC 分类</CardTitle>
          <CardDescription>按年销售额分层（A=前 80%，B=次 15%，C=末 5%）</CardDescription>
        </div>
        <button
          onClick={handleExport}
          disabled={!data}
          className="rounded-md border border-border bg-background px-2 py-1 text-xs hover:bg-muted/50 disabled:opacity-40"
        >
          ↓ CSV
        </button>
      </CardHeader>
      <CardContent>
        {loading && <p className="py-4 text-center text-sm text-muted-foreground">加载中…</p>}
        {error && <p className="py-4 text-center text-sm text-red-500">{error}</p>}
        {data && !loading && (
          <div className="space-y-3">
            {(["a", "b", "c"] as const).map((tier) => {
              const t = data[tier]
              return (
                <div key={tier} className="flex items-center gap-3">
                  <span
                    className={`flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold text-white ${TIER_COLORS[tier]}`}
                  >
                    {tier.toUpperCase()}
                  </span>
                  <div className="flex-1">
                    <div className="flex justify-between text-sm">
                      <span>{t.sku_count} 个 SKU</span>
                      <span className="tabular-nums font-medium">{t.revenue_share}</span>
                    </div>
                    <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-muted">
                      <div
                        className={`h-full rounded-full ${TIER_COLORS[tier]}`}
                        style={{ width: t.revenue_share }}
                      />
                    </div>
                  </div>
                </div>
              )
            })}
            <button
              onClick={fireCTA}
              className="mt-2 w-full rounded-md bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 transition-colors"
            >
              A 类补货建议 →
            </button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ── Dead Stock Block ──────────────────────────────────────────────────────────

export function DeadStockBlock() {
  const [days, setDays] = useState(90)
  const [data, setData] = useState<DeadStockResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    setError(null)
    fetchDeadStock(days, ac.signal)
      .then(setData)
      .catch((e: Error) => { if (e.name !== "AbortError") setError(e.message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [days])

  function handleExport() {
    if (!data) return
    downloadCSV(
      data.items.map((i) => ({
        name: i.name,
        code: i.code,
        qty: i.qty,
        value_cny: i.value_cny,
        days_since_last_movement: i.days_since_last_movement,
      })),
      `dead-stock-${days}d.csv`,
    )
  }

  function fireCTA() {
    const topItems = (data?.items ?? []).slice(0, 5).map((i) => i.name).join("、")
    window.dispatchEvent(
      new CustomEvent("tally:ai-query", {
        detail: {
          query: `以下呆滞商品超过 ${days} 天未动销：${topItems || "（无）"}。请建议清仓改价方案。`,
        },
      }),
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-2 pb-2">
        <div>
          <CardTitle className="text-base">呆滞清单</CardTitle>
          <CardDescription>超过阈值天数未发生出入库的库存</CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs"
          >
            {[30, 60, 90, 180].map((d) => (
              <option key={d} value={d}>{d}天</option>
            ))}
          </select>
          <button
            onClick={handleExport}
            disabled={!data}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs hover:bg-muted/50 disabled:opacity-40"
          >
            ↓ CSV
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {loading && <p className="py-4 text-center text-sm text-muted-foreground">加载中…</p>}
        {error && <p className="py-4 text-center text-sm text-red-500">{error}</p>}
        {data && !loading && (
          <div className="space-y-2">
            {data.items.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">暂无呆滞商品</p>
            ) : (
              <>
                <ul className="divide-y divide-border">
                  {data.items.slice(0, 8).map((item) => (
                    <li key={item.code} className="flex items-center justify-between py-2 gap-3">
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">{item.name}</p>
                        <p className="text-xs text-muted-foreground">
                          {item.code} · {item.days_since_last_movement}天未动
                        </p>
                      </div>
                      <div className="text-right text-xs">
                        <p className="font-mono tabular-nums">{item.qty}</p>
                        <p className="text-muted-foreground">¥{item.value_cny}</p>
                      </div>
                    </li>
                  ))}
                </ul>
                {data.count > 8 && (
                  <p className="text-center text-xs text-muted-foreground">
                    还有 {data.count - 8} 项，导出 CSV 查看全部
                  </p>
                )}
                <button
                  onClick={fireCTA}
                  className="mt-2 w-full rounded-md bg-orange-500/10 px-3 py-1.5 text-xs font-medium text-orange-700 hover:bg-orange-500/20 transition-colors dark:text-orange-400"
                >
                  建议清仓改价 →
                </button>
              </>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ── Sales Top-N Block ─────────────────────────────────────────────────────────

const METRIC_LABELS: Record<SalesMetric, string> = {
  revenue: "销售额",
  margin: "毛利率",
  qty: "销量",
}

export function SalesTopBlock() {
  const [metric, setMetric] = useState<SalesMetric>("revenue")
  const [days, setDays] = useState(7)
  const [data, setData] = useState<SalesTopResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    setError(null)
    fetchSalesTop(metric, days, 10, ac.signal)
      .then(setData)
      .catch((e: Error) => { if (e.name !== "AbortError") setError(e.message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [metric, days])

  function handleExport() {
    if (!data) return
    downloadCSV(
      data.top_products.map((p) => ({ rank: p.rank, name: p.name, [metric]: p.score })),
      `sales-top-${metric}-${days}d.csv`,
    )
  }

  function fireCTA() {
    const topName = data?.top_products[0]?.name ?? ""
    window.dispatchEvent(
      new CustomEvent("tally:ai-query", {
        detail: {
          query: `销售 TOP 商品"${topName}"库存是否充足？是否需要补货？`,
        },
      }),
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-2 pb-2">
        <div>
          <CardTitle className="text-base">销售 TopN</CardTitle>
          <CardDescription>指定指标 + 时间段内排名最高的商品</CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={metric}
            onChange={(e) => setMetric(e.target.value as SalesMetric)}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs"
          >
            {(["revenue", "margin", "qty"] as SalesMetric[]).map((m) => (
              <option key={m} value={m}>{METRIC_LABELS[m]}</option>
            ))}
          </select>
          <select
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs"
          >
            {[7, 14, 30, 90].map((d) => (
              <option key={d} value={d}>{d}天</option>
            ))}
          </select>
          <button
            onClick={handleExport}
            disabled={!data}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs hover:bg-muted/50 disabled:opacity-40"
          >
            ↓ CSV
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {loading && <p className="py-4 text-center text-sm text-muted-foreground">加载中…</p>}
        {error && <p className="py-4 text-center text-sm text-red-500">{error}</p>}
        {data && !loading && (
          <div className="space-y-2">
            {data.top_products.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">暂无销售数据</p>
            ) : (
              <>
                <ol className="divide-y divide-border">
                  {data.top_products.map((p) => (
                    <li key={p.rank} className="flex items-center gap-3 py-2">
                      <span className="w-5 text-center text-xs font-bold tabular-nums text-muted-foreground">
                        {p.rank}
                      </span>
                      <span className="flex-1 truncate text-sm">{p.name}</span>
                      <span className="tabular-nums text-sm font-medium">{p.score}</span>
                    </li>
                  ))}
                </ol>
                <button
                  onClick={fireCTA}
                  className="mt-2 w-full rounded-md bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 transition-colors"
                >
                  补货建议 →
                </button>
              </>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
