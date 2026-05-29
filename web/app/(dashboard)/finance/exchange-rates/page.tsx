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
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"

const MAJOR_PAIRS = ["USD", "EUR", "GBP", "JPY", "HKD"]

const SELECT_CLASS =
  "h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

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
      <div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
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
    <svg viewBox={`0 0 ${W} ${H}`} className="h-32 w-full" aria-label="30 天汇率折线图">
      <polyline
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        className="text-primary"
        points={points.join(" ")}
      />
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
  const tenantId = useTenantId()

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

  // Load current effective rates for the 5 major pairs
  useAbortableEffect((signal, isCancelled) => {
    Promise.all(
      MAJOR_PAIRS.map(async (from) => {
        const result = await getRateOn(from, "CNY", today, signal)
        return {
          from,
          rate: result.rate,
          source: result.source,
          effectiveAt: result.warning === "no_rate_found" ? "—" : today,
        } satisfies CurrentRate
      })
    ).then((rows) => {
      if (isCancelled()) return
      setCurrentRates(rows)
      setLoadingRates(false)
    }).catch(() => {
      if (isCancelled() || signal.aborted) return
      setLoadingRates(false)
    })
  }, [today])

  // Load 30-day history for selected currency
  useAbortableEffect((signal, isCancelled) => {
    setLoadingHistory(true)
    getRateHistory(historyCurrency, "CNY", 30, signal)
      .then((rates) => {
        if (isCancelled()) return
        setHistoryRates(rates)
        setLoadingHistory(false)
      })
      .catch(() => {
        if (isCancelled() || signal.aborted) return
        setLoadingHistory(false)
      })
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
      await createRate(body, tenantId)
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
    <PageContainer width="default">
      <PageHeader
        title="汇率管理"
        subtitle="手工录入各外币对人民币汇率"
        actions={<Button onClick={() => setShowModal(true)}>录入今日汇率</Button>}
      />

      <div className="space-y-8">
        {/* Current effective rates table */}
        <section>
          <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
            当前生效汇率
          </h2>
          {loadingRates ? (
            <div className="text-sm text-muted-foreground">加载中...</div>
          ) : (
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className="py-2 pr-4 text-left font-medium text-muted-foreground">货币</th>
                  <th className="py-2 pr-4 text-right font-medium text-muted-foreground">汇率（→ CNY）</th>
                  <th className="py-2 pr-4 text-right font-medium text-muted-foreground">生效日期</th>
                  <th className="py-2 text-right font-medium text-muted-foreground">来源</th>
                </tr>
              </thead>
              <tbody>
                {currentRates.map((row) => (
                  <tr key={row.from} className="border-b border-border/50">
                    <td className="py-2 pr-4 font-medium">{row.from}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{row.rate}</td>
                    <td className="py-2 pr-4 text-right text-muted-foreground">{row.effectiveAt}</td>
                    <td className="py-2 text-right">
                      <Badge tone={row.source === "default" ? "warn" : "ok"}>
                        {row.source === "default" ? "未录入" : row.source}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>

        {/* 30-day history chart */}
        <section>
          <div className="mb-3 flex items-center gap-4">
            <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              历史走势（30 天）
            </h2>
            <select
              aria-label="选择货币"
              value={historyCurrency}
              onChange={(e) => setHistoryCurrency(e.target.value)}
              className={SELECT_CLASS}
            >
              {MAJOR_PAIRS.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </div>
          {loadingHistory ? (
            <div className="text-sm text-muted-foreground">加载中...</div>
          ) : (
            <div className="rounded-xl border border-border bg-card p-4">
              <p className="mb-2 text-xs text-muted-foreground">
                {historyCurrency} → CNY（近 30 天，共 {historyRates.length} 条）
              </p>
              <RateLineChart rates={historyRates} />
            </div>
          )}
        </section>
      </div>

      {/* Create rate modal */}
      <Modal
        open={showModal}
        onOpenChange={(o) => {
          if (!o) {
            setShowModal(false)
            setModalError("")
          }
        }}
        title="录入今日汇率"
      >
        <form onSubmit={handleCreateRate} className="mt-4 space-y-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rate-from">外币</Label>
            <CurrencySelector value={modalFrom} onChange={setModalFrom} className={SELECT_CLASS} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rate-value">汇率（→ CNY）</Label>
            <Input
              id="rate-value"
              type="text"
              value={modalRate}
              onChange={(e) => setModalRate(e.target.value.replace(/[^0-9.]/g, ""))}
              placeholder="如 7.25"
              inputMode="decimal"
              required
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rate-date">生效日期</Label>
            <Input
              id="rate-date"
              type="date"
              value={modalDate}
              onChange={(e) => setModalDate(e.target.value)}
              required
            />
          </div>
          {modalError && <p className="text-sm text-destructive">{modalError}</p>}
          <div className="flex justify-end gap-3 pt-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                setShowModal(false)
                setModalError("")
              }}
            >
              取消
            </Button>
            <Button type="submit" size="sm" disabled={modalSaving}>
              {modalSaving ? "保存中..." : "保存"}
            </Button>
          </div>
        </form>
      </Modal>
    </PageContainer>
  )
}
