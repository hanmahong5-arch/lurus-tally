"use client"

import { useCallback, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import Link from "next/link"
import type { ColumnDef } from "@tanstack/react-table"
import { listStockSnapshots, type StockSnapshot } from "@/lib/api/stock"
import { listProducts, type Product } from "@/lib/api/products"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { formatCNY } from "@/lib/format"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable } from "@/components/ui/data-table"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EmptyState } from "@/components/ui/empty-state"
import { cn } from "@/lib/utils"

/**
 * Stock list page — GET /api/v1/stock/snapshots.
 * Joins snapshots with the product catalogue client-side so the table can
 * display name / SKU instead of bare UUIDs.
 */
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const SELECT_CLASS =
  "h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

function formatDecimal(raw: string | undefined, fractionDigits = 3): string {
  if (!raw) return "0"
  const n = Number(raw)
  if (!Number.isFinite(n)) return raw
  return n.toFixed(fractionDigits)
}

function shortId(id: string | undefined): string {
  if (!id) return "—"
  return id.slice(0, 8)
}

export default function StockPage() {
  const router = useRouter()
  const [snapshots, setSnapshots] = useState<StockSnapshot[]>([])
  const [products, setProducts] = useState<Map<string, Product>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [warehouseFilter, setWarehouseFilter] = useState<string>("")
  const [q, setQ] = useState("")

  const load = useCallback((signal?: AbortSignal, isCancelled?: () => boolean) => {
    setLoading(true)
    setError(null)
    // Products only enriches display (name / SKU / mnemonic search). Snapshot
    // failure should error; product failure should degrade silently so the
    // user still sees stock numbers (with short-id fallback).
    Promise.allSettled([
      listStockSnapshots({ tenantId: devTenantId, limit: 200, signal, retry: 2 }),
      listProducts({ tenantId: devTenantId, limit: 200, signal, retry: 2 }),
    ])
      .then(([snapRes, prodRes]) => {
        if (isCancelled?.() || signal?.aborted) return
        if (snapRes.status === "rejected") {
          setError(String(snapRes.reason))
          return
        }
        setSnapshots(snapRes.value)
        const map = new Map<string, Product>()
        if (prodRes.status === "fulfilled") {
          for (const p of prodRes.value.items ?? []) map.set(p.id, p)
        }
        setProducts(map)
      })
      .finally(() => {
        if (isCancelled?.()) return
        setLoading(false)
      })
  }, [])

  useAbortableEffect((signal, isCancelled) => {
    load(signal, isCancelled)
  }, [load])

  const warehouseOptions = useMemo(() => {
    const set = new Set<string>()
    for (const s of snapshots) set.add(s.warehouse_id)
    return Array.from(set).sort()
  }, [snapshots])

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return snapshots.filter((s) => {
      if (warehouseFilter && s.warehouse_id !== warehouseFilter) return false
      if (!needle) return true
      const p = products.get(s.product_id)
      const haystack = `${p?.name ?? ""} ${p?.code ?? ""} ${p?.mnemonic ?? ""}`.toLowerCase()
      return haystack.includes(needle)
    })
  }, [snapshots, products, q, warehouseFilter])

  const columns: ColumnDef<StockSnapshot>[] = [
    {
      id: "product",
      header: "商品",
      cell: ({ row }) => {
        const p = products.get(row.original.product_id)
        return (
          <Link
            href={`/stock/${row.original.product_id}`}
            className="font-medium hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            {p?.name ?? `商品 ${shortId(row.original.product_id)}`}
          </Link>
        )
      },
    },
    {
      id: "sku",
      header: "SKU",
      cell: ({ row }) => (
        <span className="font-mono text-xs text-muted-foreground">
          {products.get(row.original.product_id)?.code ?? "—"}
        </span>
      ),
    },
    {
      id: "warehouse",
      header: "仓库",
      cell: ({ row }) => (
        <span className="font-mono text-xs text-muted-foreground">
          {shortId(row.original.warehouse_id)}
        </span>
      ),
    },
    {
      id: "on_hand",
      header: "在手",
      meta: { align: "right" },
      cell: ({ row }) => {
        const lowStock = Number(row.original.on_hand_qty ?? "0") <= 0
        return (
          <span className={cn("block text-right font-mono tabular-nums", lowStock && "text-warning")}>
            {formatDecimal(row.original.on_hand_qty)}
          </span>
        )
      },
    },
    {
      id: "available",
      header: "可用",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="block text-right font-mono tabular-nums">
          {formatDecimal(row.original.available_qty)}
        </span>
      ),
    },
    {
      id: "unit_cost",
      header: "单位成本",
      meta: { align: "right" },
      cell: ({ row }) => (
        <span className="block text-right font-mono tabular-nums">
          {formatCNY(row.original.unit_cost)}
        </span>
      ),
    },
    {
      id: "updated_at",
      header: "更新时间",
      cell: ({ row }) => (
        <span className="text-muted-foreground">
          {row.original.updated_at ? new Date(row.original.updated_at).toLocaleString("zh-CN") : "—"}
        </span>
      ),
    },
  ]

  const subtitle =
    filtered.length !== snapshots.length
      ? `共 ${filtered.length} 条（已筛选自 ${snapshots.length}）`
      : `共 ${filtered.length} 条`

  return (
    <PageContainer width="wide">
      <PageHeader
        title="库存"
        subtitle={subtitle}
        actions={
          <>
            {snapshots.length === 0 ? (
              <span
                title="暂无可导出数据"
                aria-disabled="true"
                className={buttonVariants({ variant: "outline", className: "pointer-events-none opacity-40" })}
              >
                导出 CSV
              </span>
            ) : (
              <a href="/api/v1/exports/stock.csv" download className={buttonVariants({ variant: "outline" })}>
                导出 CSV
              </a>
            )}
            <Button variant="outline" onClick={() => load()}>
              刷新
            </Button>
          </>
        }
      />

      <div className="mb-4 flex flex-wrap gap-2">
        <Input
          aria-label="搜索库存商品"
          className="min-w-[14rem] flex-1"
          placeholder="搜索商品名称、编码、助记码..."
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <select
          aria-label="按仓库筛选"
          value={warehouseFilter}
          onChange={(e) => setWarehouseFilter(e.target.value)}
          className={SELECT_CLASS}
        >
          <option value="">全部仓库</option>
          {warehouseOptions.map((id) => (
            <option key={id} value={id}>
              仓库 {shortId(id)}
            </option>
          ))}
        </select>
      </div>

      <DataTable
        columns={columns}
        data={filtered}
        loading={loading}
        error={error}
        getRowId={(s) => `${s.product_id}-${s.warehouse_id}`}
        onRowClick={(s) => router.push(`/stock/${s.product_id}`)}
        animateRows
        skeletonRows={5}
        empty={
          snapshots.length === 0 ? (
            <EmptyState
              title="暂无库存记录"
              description="完成一笔采购入库后这里会出现快照"
              action={
                <Link href="/purchases/new" className="text-sm text-primary hover:underline">
                  录入采购单
                </Link>
              }
            />
          ) : (
            <EmptyState title="没有匹配的库存" description="试试清空搜索或仓库筛选" />
          )
        }
      />
    </PageContainer>
  )
}
