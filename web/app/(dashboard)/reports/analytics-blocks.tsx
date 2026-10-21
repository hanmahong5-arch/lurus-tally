"use client"

import { useEffect, useState } from "react"
import { extractApiError } from "@/lib/api/errors"
import { getTodoSummary } from "@/lib/api/todo"
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

/** The 「建议」buttons call the AI assistant, which answers 503 while it is switched off. */
function useAiEnabled(): boolean {
  const [enabled, setEnabled] = useState(false)
  useEffect(() => {
    const ctrl = new AbortController()
    getTodoSummary({ signal: ctrl.signal })
      .then((s) => {
        if (!ctrl.signal.aborted) setEnabled(s?.ai_enabled === true)
      })
      .catch(() => {})
    return () => ctrl.abort()
  }, [])
  return enabled
}

// ── Gross Margin Block ────────────────────────────────────────────────────────

export function GrossMarginBlock() {
  const aiEnabled = useAiEnabled()
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
      .catch((e: unknown) => { if ((e as Error)?.name !== "AbortError") setError(extractApiError(e).message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [days])

  function handleExport() {
    if (!data) return
    const rows = [
      ...data.top10.map((p) => ({ 类别: "毛利最高", 商品: p.name, 平均毛利率: p.avg_margin })),
      ...data.bottom10.map((p) => ({ 类别: "毛利最低", 商品: p.name, 平均毛利率: p.avg_margin })),
    ]
    downloadCSV(rows, `毛利分析-近${days}天.csv`)
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
          <CardDescription>整体毛利率 + 高/低毛利前 10 名</CardDescription>
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
            ↓ 导出表格
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
            {aiEnabled && (
              <button
                onClick={fireCTA}
                className="mt-2 w-full rounded-md bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 transition-colors"
              >
                建议调价 →
              </button>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ── ABC Classification Block ──────────────────────────────────────────────────

const TIER_LABELS: Record<string, string> = { a: "畅", b: "平", c: "滞" }

const TIER_COLORS: Record<string, string> = {
  a: "bg-green-500",
  b: "bg-yellow-400",
  c: "bg-red-400",
}

export function ABCBlock() {
  const aiEnabled = useAiEnabled()
  const [data, setData] = useState<ABCResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    setError(null)
    fetchABC(ac.signal)
      .then(setData)
      .catch((e: unknown) => { if ((e as Error)?.name !== "AbortError") setError(extractApiError(e).message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [])

  function handleExport() {
    if (!data) return
    downloadCSV(
      [
        { 分档: "畅销", 商品数: data.a.sku_count, 销售额占比: data.a.revenue_share },
        { 分档: "平销", 商品数: data.b.sku_count, 销售额占比: data.b.revenue_share },
        { 分档: "滞销", 商品数: data.c.sku_count, 销售额占比: data.c.revenue_share },
      ],
      "商品ABC分类.csv",
    )
  }

  function fireCTA() {
    window.dispatchEvent(
      new CustomEvent("tally:ai-query", {
        detail: { query: "查看畅销商品库存状态，判断是否需要补货" },
      }),
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-2 pb-2">
        <div>
          <CardTitle className="text-base">畅销 / 平销 / 滞销分档</CardTitle>
          <CardDescription>按近一年销售额分档（畅销=前 80%，平销=次 15%，滞销=末 5%）</CardDescription>
        </div>
        <button
          onClick={handleExport}
          disabled={!data}
          className="rounded-md border border-border bg-background px-2 py-1 text-xs hover:bg-muted/50 disabled:opacity-40"
        >
          ↓ 导出表格
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
                    {TIER_LABELS[tier]}
                  </span>
                  <div className="flex-1">
                    <div className="flex justify-between text-sm">
                      <span>{t.sku_count} 个商品</span>
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
            {aiEnabled && (
              <button
                onClick={fireCTA}
                className="mt-2 w-full rounded-md bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 transition-colors"
              >
                畅销品补货建议 →
              </button>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ── Dead Stock Block ──────────────────────────────────────────────────────────

export function DeadStockBlock() {
  const aiEnabled = useAiEnabled()
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
      .catch((e: unknown) => { if ((e as Error)?.name !== "AbortError") setError(extractApiError(e).message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [days])

  function handleExport() {
    if (!data) return
    downloadCSV(
      data.items.map((i) => ({
        商品: i.name,
        编码: i.code,
        库存数量: i.qty,
        库存金额: i.value_cny,
        未动销天数: i.days_since_last_movement,
      })),
      `滞销库存-${days}天以上.csv`,
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
            ↓ 导出表格
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
                    还有 {data.count - 8} 项，导出表格查看全部
                  </p>
                )}
                {aiEnabled && (
                  <button
                    onClick={fireCTA}
                    className="mt-2 w-full rounded-md bg-orange-500/10 px-3 py-1.5 text-xs font-medium text-orange-700 hover:bg-orange-500/20 transition-colors dark:text-orange-400"
                  >
                    建议清仓改价 →
                  </button>
                )}
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
  const aiEnabled = useAiEnabled()
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
      .catch((e: unknown) => { if ((e as Error)?.name !== "AbortError") setError(extractApiError(e).message) })
      .finally(() => setLoading(false))
    return () => ac.abort()
  }, [metric, days])

  function handleExport() {
    if (!data) return
    downloadCSV(
      data.top_products.map((p) => ({ 名次: p.rank, 商品: p.name, [METRIC_LABELS[metric]]: p.score })),
      `销售排行-${METRIC_LABELS[metric]}-近${days}天.csv`,
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
          <CardTitle className="text-base">销售排行</CardTitle>
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
            ↓ 导出表格
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
                {aiEnabled && (
                  <button
                    onClick={fireCTA}
                    className="mt-2 w-full rounded-md bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 transition-colors"
                  >
                    补货建议 →
                  </button>
                )}
              </>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
