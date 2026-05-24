"use client"

import { useCallback, useState } from "react"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"

import {
  listReplenishSuggestions,
  type ReplenishSuggestion,
} from "@/lib/api/replenish"
import { createPurchaseBill, type BillLineItemInput } from "@/lib/api/purchase"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
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

// Group selected rows by supplier_id (null/empty supplier gets its own group).
function groupBySupplier(
  rows: ReplenishSuggestion[]
): Map<string, ReplenishSuggestion[]> {
  const map = new Map<string, ReplenishSuggestion[]>()
  for (const row of rows) {
    const key = row.supplier_id ?? "__no_supplier__"
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(row)
  }
  return map
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

    setSubmitting(true)
    const groups = groupBySupplier(chosenRows)
    let successCount = 0
    const errors: string[] = []

    for (const rows of Array.from(groups.values())) {
      const items: BillLineItemInput[] = rows
        .filter((r: ReplenishSuggestion) => parseFloat(r.suggested_qty) > 0)
        .map((r: ReplenishSuggestion, i: number) => ({
          product_id: r.product_id,
          line_no: i + 1,
          qty: r.suggested_qty,
          unit_price: "0", // draft — supplier price filled in manually
        }))

      if (items.length === 0) continue

      const partnerId = rows[0].supplier_id
      try {
        await createPurchaseBill(
          {
            partner_id: partnerId,
            items,
            remark: "补货建议自动生成草稿",
          },
          devTenantId
        )
        successCount++
      } catch (e) {
        errors.push(String(e))
      }
    }

    setSubmitting(false)

    if (errors.length > 0) {
      toast.error(`部分草稿生成失败：${errors[0]}`)
    }
    if (successCount > 0) {
      toast.success(`已生成 ${successCount} 份采购草稿，请前往"采购单管理"确认`)
      setSelected(new Set())
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
      cell: ({ row }) => (
        <span className="font-semibold tabular-nums">
          {row.original.suggested_qty}
        </span>
      ),
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
