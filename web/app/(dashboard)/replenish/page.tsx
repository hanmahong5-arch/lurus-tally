"use client"

import { useCallback, useState } from "react"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"

import {
  listReplenishSuggestions,
  draftBatch,
  type ReplenishSuggestion,
} from "@/lib/api/replenish"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { trackEvent } from "@/lib/telemetry"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable, currencyCell } from "@/components/ui/data-table"
import { Badge, type BadgeTone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { EmptyState } from "@/components/ui/empty-state"
import { formatCNY } from "@/lib/format"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

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

  const load = useCallback(
    (signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listReplenishSuggestions({ weeks: 2, tenantId: devTenantId, signal })
        .then((res) => {
          if (isCancelled?.()) return
          setSuggestions(res.items ?? [])
        })
        .catch((e) => {
          if (isCancelled?.() || signal?.aborted) return
          setError(String(e))
        })
        .finally(() => {
          if (isCancelled?.()) return
          setLoading(false)
        })
    },
    []
  )

  useAbortableEffect((signal, isCancelled) => {
    load(signal, isCancelled)
  }, [load])

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
      const result = await draftBatch({ lines: validLines }, devTenantId)
      const draftCount = result.drafts.length
      const totalLines = result.drafts.reduce((s, d) => s + d.line_count, 0)

      // North Star WAD telemetry — each batch-create counts once per draft.
      trackEvent("wad_increment", { draft_count: draftCount, line_count: totalLines })

      toast.success(
        `已生成 ${draftCount} 张采购草稿（按供应商拆单，共 ${totalLines} 行）→ 请前往"采购单管理"确认`
      )
      setSelected(new Set())
    } catch (e) {
      toast.error(`草稿生成失败：${String(e)}`)
    } finally {
      setSubmitting(false)
    }
  }

  const allSelected =
    suggestions.length > 0 && selected.size === suggestions.length

  const columns: ColumnDef<ReplenishSuggestion>[] = [
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
        const lt = row.original.lead_time_days
        return (
          <span className="tabular-nums text-muted-foreground">
            {lt != null ? `${lt}天` : "—"}
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
            ? `已选 ${selected.size} 件，预计金额 ${formatCNY(totalEstAmt)}`
            : "每周补货建议 — 按紧迫度排序"
        }
        actions={
          <Button
            onClick={handleGenerateDrafts}
            disabled={selected.size === 0 || submitting}
          >
            {submitting ? "生成中…" : "生成采购草稿"}
          </Button>
        }
      />

      <DataTable
        columns={columns}
        data={suggestions}
        loading={loading}
        error={error}
        getRowId={(r) => r.product_id}
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
