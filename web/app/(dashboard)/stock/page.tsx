"use client"

import { useEffect, useMemo, useState } from "react"
import Link from "next/link"
import {
  listStockSnapshots,
  type StockSnapshot,
} from "@/lib/api/stock"
import { listProducts, type Product } from "@/lib/api/products"

/**
 * Stock list page — GET /api/v1/stock/snapshots.
 * Joins snapshots with the product catalogue client-side so the table can
 * display name / SKU instead of bare UUIDs.
 */
const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

function formatDecimal(raw: string | undefined, fractionDigits = 3): string {
  if (!raw) return "0"
  const n = Number(raw)
  if (!Number.isFinite(n)) return raw
  return n.toFixed(fractionDigits)
}

function formatMoney(raw: string | undefined): string {
  return formatDecimal(raw, 2)
}

function shortId(id: string | undefined): string {
  if (!id) return "—"
  return id.slice(0, 8)
}

export default function StockPage() {
  const [snapshots, setSnapshots] = useState<StockSnapshot[]>([])
  const [products, setProducts] = useState<Map<string, Product>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [warehouseFilter, setWarehouseFilter] = useState<string>("")
  const [q, setQ] = useState("")

  function load() {
    setLoading(true)
    setError(null)
    Promise.all([
      listStockSnapshots({ tenantId: devTenantId, limit: 200 }),
      listProducts({ tenantId: devTenantId, limit: 200 }),
    ])
      .then(([snaps, prodResp]) => {
        setSnapshots(snaps)
        const map = new Map<string, Product>()
        for (const p of prodResp.items ?? []) map.set(p.id, p)
        setProducts(map)
      })
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
  }, [])

  // Derive the list of distinct warehouse IDs from the snapshot data so the
  // warehouse filter dropdown only offers values actually present.
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

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold">库存</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            共 {filtered.length} 条 {filtered.length !== snapshots.length && `（已筛选自 ${snapshots.length}）`}
          </p>
        </div>
        <button
          onClick={load}
          className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted transition-colors"
        >
          刷新
        </button>
      </div>

      {/* Toolbar */}
      <div className="mb-4 flex flex-wrap gap-2">
        <input
          className="flex-1 min-w-[14rem] rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          placeholder="搜索商品名称、编码、助记码..."
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <select
          value={warehouseFilter}
          onChange={(e) => setWarehouseFilter(e.target.value)}
          className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
        >
          <option value="">全部仓库</option>
          {warehouseOptions.map((id) => (
            <option key={id} value={id}>
              仓库 {shortId(id)}
            </option>
          ))}
        </select>
      </div>

      {loading && (
        <div className="py-12 text-center text-muted-foreground">加载中...</div>
      )}
      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}
      {!loading && !error && snapshots.length === 0 && (
        <div className="py-12 text-center text-muted-foreground">
          暂无库存记录。完成一笔采购入库后这里会出现快照。
        </div>
      )}
      {!loading && !error && snapshots.length > 0 && filtered.length === 0 && (
        <div className="py-12 text-center text-muted-foreground">
          没有匹配的库存。试试清空搜索或仓库筛选。
        </div>
      )}

      {!loading && filtered.length > 0 && (
        <div className="overflow-hidden rounded-xl border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 text-left font-medium">商品</th>
                <th className="px-4 py-2.5 text-left font-medium">SKU</th>
                <th className="px-4 py-2.5 text-left font-medium">仓库</th>
                <th className="px-4 py-2.5 text-right font-medium">在手</th>
                <th className="px-4 py-2.5 text-right font-medium">可用</th>
                <th className="px-4 py-2.5 text-right font-medium">单位成本</th>
                <th className="px-4 py-2.5 text-left font-medium">更新时间</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {filtered.map((s) => {
                const p = products.get(s.product_id)
                const onHand = Number(s.on_hand_qty ?? "0")
                const lowStock = onHand <= 0
                return (
                  <tr
                    key={`${s.product_id}-${s.warehouse_id}`}
                    className="hover:bg-muted/30 transition-colors cursor-pointer"
                    onClick={() => {
                      window.location.href = `/stock/${s.product_id}`
                    }}
                  >
                    <td className="px-4 py-2.5">
                      <Link
                        href={`/stock/${s.product_id}`}
                        className="font-medium hover:underline"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {p?.name ?? `商品 ${shortId(s.product_id)}`}
                      </Link>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                      {p?.code ?? "—"}
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                      {shortId(s.warehouse_id)}
                    </td>
                    <td
                      className={`px-4 py-2.5 text-right font-mono ${lowStock ? "text-amber-600" : ""}`}
                    >
                      {formatDecimal(s.on_hand_qty)}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">
                      {formatDecimal(s.available_qty)}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">
                      {formatMoney(s.unit_cost)}
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {s.updated_at ? new Date(s.updated_at).toLocaleString("zh-CN") : "—"}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
