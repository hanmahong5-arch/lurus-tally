"use client"

import { useState } from "react"
import { useParams } from "next/navigation"
import Link from "next/link"
import {
  getProductStock,
  listStockMovements,
  type StockSnapshot,
  type StockMovement,
  type Direction,
} from "@/lib/api/stock"
import { getProduct, type Product } from "@/lib/api/products"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { formatCNY } from "@/lib/format"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const DIRECTION_LABEL: Record<Direction, string> = {
  in: "入库",
  out: "出库",
  adjust: "盘点",
}

const DIRECTION_BADGE: Record<Direction, string> = {
  in: "bg-green-500/10 text-green-600",
  out: "bg-red-500/10 text-red-500",
  adjust: "bg-blue-500/10 text-blue-600",
}

const REFERENCE_LABEL: Record<string, string> = {
  purchase: "采购",
  sale: "销售",
  adjust: "盘点",
  transfer: "调拨",
  init: "初始化",
}

function formatDecimal(raw: string | undefined, fractionDigits = 3): string {
  if (!raw) return "0"
  const n = Number(raw)
  if (!Number.isFinite(n)) return raw
  return n.toFixed(fractionDigits)
}

function shortId(id: string | undefined | null): string {
  if (!id) return "—"
  return id.slice(0, 8)
}

/**
 * Build the inbound href for a movement reference. Currently we only have
 * detail pages for purchase / sale bills; for other reference types we render
 * a plain label.
 */
function referenceHref(refType: string, refId: string | null | undefined): string | null {
  if (!refId) return null
  if (refType === "purchase") return `/purchases/${refId}`
  if (refType === "sale") return `/sales/${refId}`
  return null
}

export default function StockDetailPage() {
  const params = useParams()
  const productId = params?.product_id as string

  const [product, setProduct] = useState<Product | null>(null)
  const [snapshots, setSnapshots] = useState<StockSnapshot[]>([])
  const [movements, setMovements] = useState<StockMovement[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useAbortableEffect((signal, isCancelled) => {
    if (!productId) return
    setLoading(true)
    setError(null)
    Promise.all([
      getProduct(productId, devTenantId, signal).catch(() => null),
      getProductStock(productId, devTenantId, signal),
      listStockMovements({ product_id: productId, limit: 50, tenantId: devTenantId, signal }),
    ])
      .then(([p, snaps, mvs]) => {
        if (isCancelled()) return
        setProduct(p)
        setSnapshots(snaps)
        setMovements(mvs)
      })
      .catch((e) => {
        if (isCancelled() || signal.aborted) return
        setError(String(e))
      })
      .finally(() => {
        if (isCancelled()) return
        setLoading(false)
      })
  }, [productId])

  // Aggregate total on-hand across warehouses for the header card.
  const totalOnHand = snapshots.reduce(
    (acc, s) => acc + (Number(s.on_hand_qty ?? "0") || 0),
    0,
  )
  const totalAvailable = snapshots.reduce(
    (acc, s) => acc + (Number(s.available_qty ?? "0") || 0),
    0,
  )

  if (loading) {
    return <div className="p-6 text-muted-foreground text-sm">加载中...</div>
  }

  if (error) {
    return (
      <div className="p-6 space-y-4">
        <ErrorBanner hint="请刷新页面重试">{error}</ErrorBanner>
        <Link href="/stock" className="text-sm text-primary hover:underline">
          返回库存列表
        </Link>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      {/* Header card */}
      <div>
        <Link
          href="/stock"
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          ← 返回库存列表
        </Link>
        <div className="mt-2 rounded-xl border border-border bg-card p-5">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h1 className="text-xl font-semibold">
                {product?.name ?? `商品 ${shortId(productId)}`}
              </h1>
              <p className="text-sm text-muted-foreground mt-0.5 font-mono">
                {product?.code ?? productId}
                {product?.brand && ` · ${product.brand}`}
              </p>
            </div>
            <div className="text-right">
              <p className="text-xs text-muted-foreground">总在手 / 可用</p>
              <p className="text-lg font-mono font-semibold mt-0.5">
                {totalOnHand.toFixed(3)}
                <span className="text-sm text-muted-foreground"> / {totalAvailable.toFixed(3)}</span>
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* Per-warehouse table */}
      <section>
        <h2 className="text-sm font-medium text-muted-foreground mb-2">仓库分布</h2>
        {snapshots.length === 0 ? (
          <EmptyState title="该商品暂无库存" description="采购入库后将在此显示仓库分布" />
        ) : (
          <div className="overflow-x-auto rounded-xl border border-border">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">仓库</th>
                  <th className="px-4 py-2.5 text-right font-medium">在手</th>
                  <th className="px-4 py-2.5 text-right font-medium">可用</th>
                  <th className="px-4 py-2.5 text-right font-medium">单位成本</th>
                  <th className="px-4 py-2.5 text-left font-medium">更新时间</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {snapshots.map((s) => (
                  <tr key={s.warehouse_id} className="hover:bg-muted/30 transition-colors">
                    <td className="px-4 py-2.5 font-mono text-xs">
                      {shortId(s.warehouse_id)}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">
                      {formatDecimal(s.on_hand_qty)}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">
                      {formatDecimal(s.available_qty)}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono">
                      {formatCNY(s.unit_cost)}
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {s.updated_at ? new Date(s.updated_at).toLocaleString("zh-CN") : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Movement timeline */}
      <section>
        <div className="flex items-center justify-between mb-2">
          <h2 className="text-sm font-medium text-muted-foreground">
            最近变动 <span className="text-xs">（最多 50 条）</span>
          </h2>
          <Link
            href={`/stock/${productId}/timeline`}
            className="text-xs text-primary hover:underline"
          >
            查看变动历史 →
          </Link>
        </div>
        {movements.length === 0 ? (
          <EmptyState title="暂无变动记录" description="采购或销售产生入出库后将在此显示" />
        ) : (
          <ol className="space-y-2">
            {movements.map((m) => {
              const dirLabel = DIRECTION_LABEL[m.direction] ?? m.direction
              const dirClass = DIRECTION_BADGE[m.direction] ?? "bg-muted text-muted-foreground"
              const sign = m.direction === "out" ? "-" : "+"
              const refLabel = REFERENCE_LABEL[m.reference_type] ?? m.reference_type
              const href = referenceHref(m.reference_type, m.reference_id)
              return (
                <li
                  key={m.id}
                  className="rounded-lg border border-border bg-card px-4 py-3 flex items-center gap-4"
                >
                  <span
                    className={`rounded-full px-2 py-0.5 text-xs flex-shrink-0 ${dirClass}`}
                  >
                    {dirLabel}
                  </span>
                  <span className="font-mono text-sm flex-shrink-0">
                    {sign}
                    {formatDecimal(m.qty_base)}
                  </span>
                  <div className="flex-1 min-w-0 text-xs text-muted-foreground">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span>仓库 {shortId(m.warehouse_id)}</span>
                      <span>·</span>
                      <span>
                        {refLabel}
                        {m.reference_id && (
                          <>
                            {" "}
                            {href ? (
                              <Link
                                href={href}
                                className="text-primary hover:underline font-mono"
                              >
                                {shortId(m.reference_id)}
                              </Link>
                            ) : (
                              <span className="font-mono">{shortId(m.reference_id)}</span>
                            )}
                          </>
                        )}
                      </span>
                      {m.note && (
                        <>
                          <span>·</span>
                          <span className="truncate">{m.note}</span>
                        </>
                      )}
                    </div>
                  </div>
                  <span className="text-xs text-muted-foreground flex-shrink-0">
                    {m.occurred_at
                      ? new Date(m.occurred_at).toLocaleString("zh-CN")
                      : ""}
                  </span>
                </li>
              )
            })}
          </ol>
        )}
      </section>
    </div>
  )
}
