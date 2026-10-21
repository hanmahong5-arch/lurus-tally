"use client"

import { useCallback, useState } from "react"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"

import {
  listReplenishSuggestions,
  draftBatch,
  fetchScorecard,
  type ReplenishSuggestion,
  type ReplenishScorecard,
} from "@/lib/api/replenish"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { trackEvent } from "@/lib/telemetry"
import { notifyFirstPoExported } from "@/components/onboarding/OnboardingWizard"
import { extractApiError } from "@/lib/api/errors"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable, currencyCell } from "@/components/ui/data-table"
import { DataCard } from "@/components/ui/data-card"
import { Badge, type BadgeTone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { EmptyState } from "@/components/ui/empty-state"
import { formatCNY } from "@/lib/format"

// urgency thresholds: < 3 days = err, < 7 = warn, else neutral
function urgencyTone(score: string): BadgeTone {
  const days = parseFloat(score)
  if (isNaN(days) || days > 9999) return "neutral"
  if (days < 3) return "err"
  if (days < 7) return "warn"
  return "ok"
}

function urgencyLabel(score: string): string {
  const days = parseFloat(score)
  if (isNaN(days) || days > 9999) return "无销量"
  return `${Math.floor(days)}天`
}

export default function ReplenishPage() {
  const [suggestions, setSuggestions] = useState<ReplenishSuggestion[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [submitting, setSubmitting] = useState(false)
  const [scorecard, setScorecard] = useState<ReplenishScorecard | null>(null)
  const tenantId = useTenantId()
  // Estimated amounts are cost data, and drafting needs bill.draft. An unknown role hides
  // nothing (the backend decides).
  const { known, can } = useRole()
  const showCost = !known || can("cost.view")
  const mayDraft = !known || can("bill.draft")

  const load = useCallback(
    (signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listReplenishSuggestions({ weeks: 2, tenantId, signal })
        .then((res) => {
          if (isCancelled?.()) return
          setSuggestions(res.items ?? [])
        })
        .catch((e) => {
          if (isCancelled?.() || signal?.aborted) return
          setError(extractApiError(e).message)
        })
        .finally(() => {
          if (isCancelled?.()) return
          setLoading(false)
        })
    },
    [tenantId]
  )

  useAbortableEffect((signal, isCancelled) => {
    load(signal, isCancelled)
  }, [load])

  // The track-record bar is informational: a fetch failure (old backend,
  // network blip) simply leaves the bar unrendered and the page unaffected.
  useAbortableEffect(
    (signal, isCancelled) => {
      fetchScorecard(tenantId, signal)
        .then((sc) => {
          if (isCancelled?.()) return
          setScorecard(sc)
        })
        .catch(() => {
          if (isCancelled?.()) return
          setScorecard(null)
        })
    },
    [tenantId]
  )

  function toggleRow(id: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function toggleAll() {
    if (selected.size === suggestions.length) {
      setSelected(new Set())
    } else {
      setSelected(new Set(suggestions.map((r) => r.product_id)))
    }
  }

  async function handleGenerateDrafts() {
    const chosenRows = suggestions.filter((r) => selected.has(r.product_id))
    if (chosenRows.length === 0) {
      toast.error("请先勾选需要补货的商品")
      return
    }

    const validLines = chosenRows
      .filter((r: ReplenishSuggestion) => parseFloat(r.suggested_qty) > 0)
      .map((r: ReplenishSuggestion) => ({
        product_id: r.product_id,
        supplier_id: r.supplier_id,
        qty: r.suggested_qty,
      }))

    if (validLines.length === 0) {
      toast.error("所有选中商品建议订量为 0，无需下单")
      return
    }

    setSubmitting(true)
    try {
      const result = await draftBatch({ lines: validLines }, tenantId)
      const draftCount = result.drafts.length
      const totalLines = result.drafts.reduce((s, d) => s + d.line_count, 0)

      // North Star WAD telemetry — each batch-create counts once per draft.
      trackEvent("wad_increment", { draft_count: draftCount, line_count: totalLines })

      // KS1 onboarding-completion trigger: a user's first PO draft IS the
      // onboarding aha-moment ("生成第一张采购单", OnboardingWizard step 3).
      // notifyFirstPoExported fires onboarding_first_po_exported exactly once
      // and advances the wizard to "done", so this is a no-op on later drafts.
      if (
        typeof window !== "undefined" &&
        localStorage.getItem("tally_onboarding_step") === "replenish"
      ) {
        notifyFirstPoExported()
      }

      toast.success(
        `已生成 ${draftCount} 张采购草稿（按供应商拆单，共 ${totalLines} 行）→ 请前往"采购单管理"确认`
      )
      setSelected(new Set())
    } catch (e) {
      toast.error(`草稿生成失败：${extractApiError(e).message}`)
    } finally {
      setSubmitting(false)
    }
  }

  const allSelected =
    suggestions.length > 0 && selected.size === suggestions.length

  const allColumns: ColumnDef<ReplenishSuggestion>[] = [
    {
      id: "select",
      header: () => (
        <input
          type="checkbox"
          checked={allSelected}
          onChange={toggleAll}
          aria-label="全选"
        />
      ),
      cell: ({ row }) => (
        <input
          type="checkbox"
          checked={selected.has(row.original.product_id)}
          onChange={() => toggleRow(row.original.product_id)}
          aria-label={`选择 ${row.original.product_name}`}
        />
      ),
    },
    {
      id: "product",
      header: "商品",
      cell: ({ row }) => (
        <div>
          <div className="font-medium">{row.original.product_name}</div>
          <div className="font-mono text-xs text-muted-foreground">
            {row.original.product_code}
          </div>
        </div>
      ),
    },
    {
      id: "urgency",
      header: "紧迫度",
      cell: ({ row }) => (
        <Badge tone={urgencyTone(row.original.urgency_score)}>
          {urgencyLabel(row.original.urgency_score)}
        </Badge>
      ),
    },
    {
      id: "available_qty",
      header: "可用库存",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="tabular-nums">{row.original.available_qty}</span>
      ),
    },
    {
      id: "safety_qty",
      header: "安全线",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="tabular-nums text-muted-foreground">
          {row.original.safety_qty}
        </span>
      ),
    },
    {
      id: "avg_daily_sales",
      header: "周均销量",
      meta: { align: "right" },
      cell: ({ row }) => {
        const weekly = (parseFloat(row.original.avg_daily_sales) * 7).toFixed(1)
        return <span className="tabular-nums">{weekly}</span>
      },
    },
    {
      id: "suggested_qty",
      header: "建议订量",
      meta: { align: "right" },
      cell: ({ row }) => {
        const r = row.original
        const hasDetail =
          r.rop !== undefined && r.rop !== "" && r.reason !== undefined && r.reason !== ""
        return (
          <div className="flex flex-col items-end gap-0.5">
            <span className="font-semibold tabular-nums">{r.suggested_qty}</span>
            {hasDetail && (
              <span
                className="text-[10px] text-muted-foreground cursor-help max-w-[160px] text-right leading-tight"
                title={r.reason}
              >
                ROP {parseFloat(r.rop).toFixed(1)} · 安全库存 {parseFloat(r.safety_stock ?? "0").toFixed(1)} · 在途 {r.in_transit ?? "0"}
              </span>
            )}
          </div>
        )
      },
    },
    {
      id: "lead_time",
      header: "提前期",
      meta: { align: "right" },
      cell: ({ row }) => {
        const r = row.original
        const lt = r.lead_time_days
        return (
          <span className="inline-flex items-center justify-end gap-1">
            <span className="tabular-nums text-muted-foreground">
              {lt != null ? `${lt}天` : "—"}
            </span>
            {r.lead_time_source === "learned" && (
              // Learned from actual arrivals — tooltip carries the full reason.
              <span title={r.reason} className="cursor-help">
                <Badge tone="ok">实测</Badge>
              </span>
            )}
          </span>
        )
      },
    },
    {
      id: "est_amount_cny",
      header: "预计金额",
      meta: { align: "right" },
      cell: ({ row }) => currencyCell(row.original.est_amount_cny),
    },
    {
      id: "supplier",
      header: "建议供应商",
      cell: ({ row }) =>
        row.original.supplier_name ? (
          <span className="text-sm">{row.original.supplier_name}</span>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        ),
    },
  ]

  // The select column only serves drafting.
  const columns = allColumns.filter(
    (c) => (showCost || c.id !== "est_amount_cny") && (mayDraft || c.id !== "select")
  )

  const selectedRows = suggestions.filter((r) => selected.has(r.product_id))
  const totalEstAmt = selectedRows.reduce(
    (sum, r) => sum + parseFloat(r.est_amount_cny || "0"),
    0
  )

  return (
    <PageContainer width="wide">
      <PageHeader
        title="补货决策"
        subtitle={
          selected.size > 0
            ? showCost
              ? `已选 ${selected.size} 件，预计金额 ${formatCNY(totalEstAmt)}`
              : `已选 ${selected.size} 件`
            : "每周补货建议 — 按紧迫度排序"
        }
        actions={
          mayDraft ? (
            <Button
              onClick={handleGenerateDrafts}
              disabled={selected.size === 0 || submitting}
            >
              {submitting ? "生成中…" : "生成采购草稿"}
            </Button>
          ) : undefined
        }
      />

      {scorecard && scorecard.suggestions_count > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-muted/30 px-3 py-2 text-sm">
          <span className="text-muted-foreground">
            近 4 周建议 {scorecard.suggestions_count} 项
          </span>
          <Badge tone="ok">
            采纳率 {Math.round(scorecard.adoption_rate * 100)}%
          </Badge>
          {scorecard.stockout_misses > 0 && (
            <Badge tone="err">
              未采纳且断货 {scorecard.stockout_misses} 项
            </Badge>
          )}
        </div>
      )}

      <DataTable
        columns={columns}
        data={suggestions}
        loading={loading}
        error={error}
        getRowId={(r) => r.product_id}
        mobileCard={(r) => (
          <DataCard
            title={
              <span className="flex items-center gap-2">
                {mayDraft && (
                  <input
                    type="checkbox"
                    checked={selected.has(r.product_id)}
                    onChange={() => toggleRow(r.product_id)}
                    onClick={(e) => e.stopPropagation()}
                    aria-label={`手机端选择 ${r.product_name}`}
                  />
                )}
                <span className="truncate">{r.product_name}</span>
              </span>
            }
            subtitle={r.product_code}
            aside={
              <Badge tone={urgencyTone(r.urgency_score)}>{urgencyLabel(r.urgency_score)}</Badge>
            }
            fields={[
              { label: "可用库存", value: r.available_qty },
              { label: "安全线", value: r.safety_qty },
              { label: "建议订量", value: <span className="font-semibold">{r.suggested_qty}</span> },
              ...(showCost ? [{ label: "预计金额", value: currencyCell(r.est_amount_cny) }] : []),
              { label: "建议供应商", value: r.supplier_name || "—" },
            ]}
          />
        )}
        animateRows
        empty={
          <EmptyState
            title="暂无补货建议"
            description="请先连接数据 · 导入商品 · 记录销售订单，系统将自动计算补货量"
          />
        }
      />
    </PageContainer>
  )
}
