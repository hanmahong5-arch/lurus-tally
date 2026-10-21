"use client"

import { useCallback, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import Link from "next/link"
import type { ColumnDef } from "@tanstack/react-table"
import { listStockSnapshots, type StockSnapshot } from "@/lib/api/stock"
import { listProducts, type Product } from "@/lib/api/products"
import { listWarehouses } from "@/lib/api/warehouses"
import { extractApiError } from "@/lib/api/errors"
import { StockAdjustDialog, type CountWarehouse } from "@/components/stock-adjust-dialog"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { formatCNY } from "@/lib/format"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable, statusCell } from "@/components/ui/data-table"
import { DataCard } from "@/components/ui/data-card"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EmptyState } from "@/components/ui/empty-state"
import { cn } from "@/lib/utils"

/**
 * Stock list page — GET /api/v1/stock/snapshots.
 * Joins snapshots with the product catalogue client-side so the table can
 * display name / SKU instead of bare UUIDs.
 */
const SELECT_CLASS =
  "h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

function formatDecimal(raw: string | undefined, fractionDigits = 3): string {
  if (!raw) return "0"
  const n = Number(raw)
  if (!Number.isFinite(n)) return raw
  return n.toFixed(fractionDigits)
}

export default function StockPage() {
  const router = useRouter()
  const [snapshots, setSnapshots] = useState<StockSnapshot[]>([])
  const [products, setProducts] = useState<Map<string, Product>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [warehouses, setWarehouses] = useState<CountWarehouse[]>([])
  const [countOpen, setCountOpen] = useState(false)
  const [warehouseFilter, setWarehouseFilter] = useState<string>("")
  const [q, setQ] = useState("")
  const tenantId = useTenantId()

  const load = useCallback((signal?: AbortSignal, isCancelled?: () => boolean) => {
    setLoading(true)
    setError(null)
    // Products only enriches display (name / SKU / mnemonic search). Snapshot
    // failure should error; product failure should degrade silently so the
    // user still sees stock numbers (with short-id fallback).
    Promise.allSettled([
      listStockSnapshots({ tenantId, limit: 500, signal, retry: 2 }),
      listProducts({ tenantId, limit: 500, signal, retry: 2 }),
      listWarehouses({ tenantId, limit: 200, signal, retry: 2 }),
    ])
      .then(([snapRes, prodRes, whRes]) => {
        if (isCancelled?.() || signal?.aborted) return
        if (snapRes.status === "rejected") {
          setError(extractApiError(snapRes.reason).message)
          return
        }
        setSnapshots(snapRes.value)
        if (whRes.status === "fulfilled") {
          setWarehouses(
            (whRes.value.items ?? []).map((w) => ({
              id: w.id,
              name: w.name,
              isDefault: Boolean((w as { is_default?: boolean }).is_default ?? w.isDefault),
            }))
          )
        }
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
  }, [tenantId])

  useAbortableEffect((signal, isCancelled) => {
    load(signal, isCancelled)
  }, [load])

  const warehouseNames = useMemo(() => new Map(warehouses.map((w) => [w.id, w.name])), [warehouses])
  const warehouseName = (id: string) => warehouseNames.get(id) ?? "未知仓库"

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

  // Unit cost is hidden from roles without cost.view; unknown role hides nothing (the backend trims it).
  const { known, can } = useRole()
  const showCost = !known || can("cost.view")
  const mayExport = !known || can("export.stock")
  const mayAdjust = !known || can("stock.adjust")
  const mayDraft = !known || can("bill.draft")

  const allColumns: ColumnDef<StockSnapshot>[] = [
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
            {p?.name ?? "未知商品"}
          </Link>
        )
      },
    },
    {
      id: "sku",
      header: "商品编码",
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
        <span className="text-muted-foreground">{warehouseName(row.original.warehouse_id)}</span>
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
  const columns = allColumns.filter((c) => showCost || c.id !== "unit_cost")

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
            {!mayExport ? null : snapshots.length === 0 ? (
              <span
                title="暂无可导出数据"
                aria-disabled="true"
                className={buttonVariants({ variant: "outline", className: "pointer-events-none opacity-40" })}
              >
                导出表格
              </span>
            ) : (
              <a href="/api/proxy/exports/stock.csv" download className={buttonVariants({ variant: "outline" })}>
                导出表格
              </a>
            )}
            {mayAdjust && (
              <Button variant="outline" onClick={() => setCountOpen(true)}>
                盘点
              </Button>
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
              {warehouseName(id)}
            </option>
          ))}
        </select>
      </div>

      <StockAdjustDialog
        open={countOpen}
        onOpenChange={setCountOpen}
        tenantId={tenantId}
        products={Array.from(products.values())}
        snapshots={snapshots}
        warehouses={warehouses}
        onDone={() => load()}
      />

      <DataTable
        columns={columns}
        data={filtered}
        loading={loading}
        error={error}
        getRowId={(s) => `${s.product_id}-${s.warehouse_id}`}
        onRowClick={(s) => router.push(`/stock/${s.product_id}`)}
        mobileCard={(s) => {
          const p = products.get(s.product_id)
          const lowStock = Number(s.on_hand_qty ?? "0") <= 0
          return (
            <DataCard
              title={p?.name ?? "未知商品"}
              subtitle={p?.code ?? undefined}
              aside={lowStock ? statusCell("低库存", "warn") : undefined}
              fields={[
                { label: "在手", value: formatDecimal(s.on_hand_qty) },
                { label: "可用", value: formatDecimal(s.available_qty) },
                { label: "仓库", value: warehouseName(s.warehouse_id) },
                ...(showCost ? [{ label: "单位成本", value: formatCNY(s.unit_cost) }] : []),
              ]}
            />
          )
        }}
        animateRows
        skeletonRows={5}
        empty={
          snapshots.length === 0 ? (
            <EmptyState
              title="暂无库存记录"
              description={
                mayDraft
                  ? "新店建账请到「商品」页点「批量导入」，一次录入商品和期初库存；也可以完成一笔采购入库"
                  : "还没有库存，等老板或店长录入后这里就会出现"
              }
              action={
                mayDraft ? (
                  <Link href="/purchases/new" className="text-sm text-primary hover:underline">
                    录入采购单
                  </Link>
                ) : undefined
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
