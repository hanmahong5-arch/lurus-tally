"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { useProfile } from "@/lib/profile"
import {
  getRateOn,
  createRate,
  getRateHistory,
  type ExchangeRate,
  type CreateRateRequest,
} from "@/lib/api/currency"
import { CurrencySelector } from "@/components/cross-border/currency-selector"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID
const MAJOR_PAIRS = ["USD", "EUR", "GBP", "JPY", "HKD"]

// Current rates table row
interface CurrentRate {
  from: string
  rate: string
  source: string
  effectiveAt: string
}

// Simple SVG line chart for rate history
function RateLineChart({ rates }: { rates: ExchangeRate[] }) {
  if (rates.length < 2) {
    return (
      <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
        数据不足，无法绘制折线图（需至少 2 条记录）
      </div>
    )
  }

  const values = rates.map((r) => parseFloat(r.rate))
  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1

  const W = 600
  const H = 120
  const padX = 20
  const padY = 10

  const points = rates.map((r, i) => {
    const x = padX + (i / (rates.length - 1)) * (W - padX * 2)
    const y = padY + ((max - parseFloat(r.rate)) / range) * (H - padY * 2)
    return `${x.toFixed(1)},${y.toFixed(1)}`
  })

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      className="w-full h-32"
      aria-label="30 天汇率折线图"
    >
      <polyline
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        className="text-primary"
        points={points.join(" ")}
      />
      {/* Min / Max labels */}
      <text x={padX} y={padY} className="text-xs" fontSize="10" fill="currentColor">
        {max.toFixed(4)}
      </text>
      <text x={padX} y={H - padY + 10} fontSize="10" fill="currentColor">
        {min.toFixed(4)}
      </text>
    </svg>
  )
}

export default function ExchangeRatesPage() {
  const { profileType } = useProfile()
  const router = useRouter()

  // Route guard: only cross_border and hybrid profiles can access this page.
  useEffect(() => {
    if (profileType !== null && profileType !== "cross_border" && profileType !== "hybrid") {
      router.replace("/dashboard")
    }
  }, [profileType, router])

  const today = new Date().toISOString().slice(0, 10)

  const [currentRates, setCurrentRates] = useState<CurrentRate[]>([])
  const [loadingRates, setLoadingRates] = useState(true)

  const [historyCurrency, setHistoryCurrency] = useState("USD")
  const [historyRates, setHistoryRates] = useState<ExchangeRate[]>([])
  const [loadingHistory, setLoadingHistory] = useState(false)

  // Create rate modal state
  const [showModal, setShowModal] = useState(false)
  const [modalFrom, setModalFrom] = useState("USD")
  const [modalTo] = useState("CNY")
  const [modalRate, setModalRate] = useState("")
  const [modalDate, setModalDate] = useState(today)
  const [modalError, setModalError] = useState("")
  const [modalSaving, setModalSaving] = useState(false)

  const inputCls =
    "w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"

  // Load current effective rates for the 5 major pairs
  useEffect(() => {
    let cancelled = false
    Promise.all(
      MAJOR_PAIRS.map(async (from) => {
        const result = await getRateOn(from, "CNY", today)
        return {
          from,
          rate: result.rate,
          source: result.source,
          effectiveAt: result.warning === "no_rate_found" ? "—" : today,
        } satisfies CurrentRate
      })
    ).then((rows) => {
      if (!cancelled) {
        setCurrentRates(rows)
        setLoadingRates(false)
      }
    }).catch(() => {
      if (!cancelled) setLoadingRates(false)
    })
    return () => { cancelled = true }
  }, [today])

  // Load 30-day history for selected currency
  useEffect(() => {
    let cancelled = false
    setLoadingHistory(true)
    getRateHistory(historyCurrency, "CNY", 30)
      .then((rates) => {
        if (!cancelled) {
          setHistoryRates(rates)
          setLoadingHistory(false)
        }
      })
      .catch(() => {
        if (!cancelled) setLoadingHistory(false)
      })
    return () => { cancelled = true }
  }, [historyCurrency])

  async function handleCreateRate(e: React.FormEvent) {
    e.preventDefault()
    setModalError("")
    if (!modalRate || parseFloat(modalRate) <= 0) {
      setModalError("请输入有效汇率（大于 0）")
      return
    }
    setModalSaving(true)
    try {
      const body: CreateRateRequest = {
        from_currency: modalFrom,
        to_currency: modalTo,
        rate: modalRate,
        effective_at: new Date(modalDate).toISOString(),
      }
      await createRate(body, devTenantId)
      setShowModal(false)
      setModalRate("")
      // Refresh current rates
      setLoadingRates(true)
      const rows = await Promise.all(
        MAJOR_PAIRS.map(async (from) => {
          const result = await getRateOn(from, "CNY", today)
          return { from, rate: result.rate, source: result.source, effectiveAt: today } satisfies CurrentRate
        })
      )
      setCurrentRates(rows)
      setLoadingRates(false)
    } catch (err) {
      setModalError(String(err))
    } finally {
      setModalSaving(false)
    }
  }

  if (profileType !== null && profileType !== "cross_border" && profileType !== "hybrid") {
    return null
  }

  return (
    <div className="p-6 max-w-4xl mx-auto space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">汇率管理</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            手工录入各外币对人民币汇率
          </p>
        </div>
        <button
          onClick={() => setShowModal(true)}
          className="rounded-lg bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          录入今日汇率
        </button>
      </div>

      {/* Current effective rates table */}
      <section>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground mb-3">
          当前生效汇率
        </h2>
        {loadingRates ? (
          <div className="text-sm text-muted-foreground">加载中...</div>
        ) : (
          <table className="w-full text-sm border-collapse">
            <thead>
              <tr className="border-b border-border">
                <th className="text-left py-2 pr-4 font-medium text-muted-foreground">货币</th>
                <th className="text-right py-2 pr-4 font-medium text-muted-foreground">汇率（→ CNY）</th>
                <th className="text-right py-2 pr-4 font-medium text-muted-foreground">生效日期</th>
                <th className="text-right py-2 font-medium text-muted-foreground">来源</th>
              </tr>
            </thead>
            <tbody>
              {currentRates.map((row) => (
                <tr key={row.from} className="border-b border-border/50">
                  <td className="py-2 pr-4 font-medium">{row.from}</td>
                  <td className="py-2 pr-4 text-right tabular-nums">{row.rate}</td>
                  <td className="py-2 pr-4 text-right text-muted-foreground">{row.effectiveAt}</td>
                  <td className="py-2 text-right">
                    <span
                      className={`inline-flex items-center rounded px-1.5 py-0.5 text-xs ${
                        row.source === "default"
                          ? "bg-yellow-100 text-yellow-700"
                          : "bg-green-100 text-green-700"
                      }`}
                    >
                      {row.source === "default" ? "未录入" : row.source}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {/* 30-day history chart */}
      <section>
        <div className="flex items-center gap-4 mb-3">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
            历史走势（30 天）
          </h2>
          <select
            value={historyCurrency}
            onChange={(e) => setHistoryCurrency(e.target.value)}
            className="rounded-md border border-input bg-background px-2 py-1 text-sm"
          >
            {MAJOR_PAIRS.map((c) => (
              <option key={c} value={c}>{c}</option>
            ))}
          </select>
        </div>
        {loadingHistory ? (
          <div className="text-sm text-muted-foreground">加载中...</div>
        ) : (
          <div className="rounded-xl border border-border bg-card p-4">
            <p className="text-xs text-muted-foreground mb-2">
              {historyCurrency} → CNY（近 30 天，共 {historyRates.length} 条）
            </p>
            <RateLineChart rates={historyRates} />
          </div>
        )}
      </section>

      {/* Create rate modal */}
      {showModal && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
          role="dialog"
          aria-modal="true"
          aria-label="录入汇率"
        >
          <div className="bg-card rounded-xl border border-border p-6 w-full max-w-sm shadow-lg">
            <h3 className="text-base font-semibold mb-4">录入今日汇率</h3>
            <form onSubmit={handleCreateRate} className="space-y-4">
              <div>
                <label className="text-sm font-medium block mb-1">外币</label>
                <CurrencySelector
                  value={modalFrom}
                  onChange={setModalFrom}
                  className={inputCls}
                />
              </div>
              <div>
                <label className="text-sm font-medium block mb-1">汇率（→ CNY）</label>
                <input
                  type="text"
                  className={inputCls}
                  value={modalRate}
                  onChange={(e) => setModalRate(e.target.value.replace(/[^0-9.]/g, ""))}
                  placeholder="如 7.25"
                  inputMode="decimal"
                  required
                />
              </div>
              <div>
                <label className="text-sm font-medium block mb-1">生效日期</label>
                <input
                  type="date"
                  className={inputCls}
                  value={modalDate}
                  onChange={(e) => setModalDate(e.target.value)}
                  required
                />
              </div>
              {modalError && (
                <p className="text-sm text-destructive">{modalError}</p>
              )}
              <div className="flex justify-end gap-3 pt-2">
                <button
                  type="button"
                  onClick={() => { setShowModal(false); setModalError("") }}
                  className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted transition-colors"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={modalSaving}
                  className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-60"
                >
                  {modalSaving ? "保存中..." : "保存"}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
