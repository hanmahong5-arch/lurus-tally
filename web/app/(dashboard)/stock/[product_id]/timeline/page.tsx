/**
 * Stock movement timeline page — full ledger for a single product.
 *
 * Route: /stock/[product_id]/timeline
 *
 * Renders all recorded stock movements via a shadcn-style timeline list.
 * Each row shows: time · direction badge · qty · unit cost · source reference
 * (linked to the originating bill when available).
 */

import Link from "next/link"
import { auth } from "@/auth"
import { type StockMovement, type Direction, type ReferenceType } from "@/lib/api/stock"

// Re-export cache policy so Next.js does not statically pre-render this page.
export const revalidate = 60

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

const REFERENCE_LABEL: Record<ReferenceType, string> = {
  purchase: "采购",
  sale: "销售",
  adjust: "盘点",
  transfer: "调拨",
  init: "初始化",
}

function referenceHref(refType: ReferenceType, refId: string | null | undefined): string | null {
  if (!refId) return null
  if (refType === "purchase") return `/purchases/${refId}`
  if (refType === "sale") return `/sales/${refId}`
  return null
}

function shortId(id: string | undefined | null): string {
  if (!id) return "—"
  return id.slice(0, 8)
}

function formatDecimal(raw: string | undefined, fractionDigits = 3): string {
  if (!raw) return "0"
  const n = Number(raw)
  if (!Number.isFinite(n)) return raw
  return n.toFixed(fractionDigits)
}

function formatDateTime(iso: string | undefined | null): string {
  if (!iso) return "—"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return "—"
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(d)
}

interface Props {
  params: Promise<{ product_id: string }>
}

export default async function StockTimelinePage({ params }: Props) {
  const { product_id: productId } = await params
  const session = await auth()

  let movements: StockMovement[] = []
  let fetchError = false

  if (session?.accessToken) {
    try {
      // Use the browser-side client via the /api/proxy path when running
      // server-side in the same Next.js process: call listStockMovements
      // using the server BACKEND_URL directly to avoid the proxy round-trip.
      const BACKEND_URL =
        process.env.BACKEND_URL ?? "http://tally-backend:18200"
      const qs = new URLSearchParams({ product_id: productId, limit: "200" })
      const res = await fetch(`${BACKEND_URL}/api/v1/stock/movements?${qs.toString()}`, {
        headers: { Authorization: `Bearer ${session.accessToken}` },
        next: { revalidate: 60 },
      })
      if (res.ok) {
        const body = (await res.json()) as { items?: StockMovement[] }
        movements = body.items ?? []
      } else {
        fetchError = true
      }
    } catch {
      fetchError = true
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-2 text-xs text-muted-foreground">
        <Link href="/stock" className="hover:text-foreground transition-colors">
          库存总览
        </Link>
        <span aria-hidden="true">›</span>
        <Link
          href={`/stock/${productId}`}
          className="hover:text-foreground transition-colors font-mono"
        >
          {shortId(productId)}
        </Link>
        <span aria-hidden="true">›</span>
        <span className="text-foreground">变动历史</span>
      </nav>

      <header>
        <h1 className="text-xl font-semibold">库存变动历史</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          商品 <span className="font-mono">{shortId(productId)}</span> 的完整出入库记录（最多 200 条，最新在前）
        </p>
      </header>

      {fetchError && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          加载变动记录失败，请刷新重试。
        </div>
      )}

      {!fetchError && movements.length === 0 && (
        <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border bg-muted/20 px-6 py-12 text-center">
          <p className="text-sm font-medium text-foreground">暂无库存变动记录</p>
          <p className="max-w-sm text-xs text-muted-foreground">
            该商品尚未产生任何出入库。创建采购单并审核后将在此记录。
          </p>
          <Link
            href="/purchases/new"
            className="mt-2 text-xs text-primary hover:underline"
          >
            新建采购单 →
          </Link>
        </div>
      )}

      {movements.length > 0 && (
        <ol className="space-y-2">
          {movements.map((m) => {
            const dirLabel = DIRECTION_LABEL[m.direction] ?? m.direction
            const dirClass = DIRECTION_BADGE[m.direction] ?? "bg-muted text-muted-foreground"
            const sign = m.direction === "out" ? "−" : "+"
            const refLabel = REFERENCE_LABEL[m.reference_type] ?? m.reference_type
            const href = referenceHref(m.reference_type, m.reference_id)

            return (
              <li
                key={m.id}
                className="rounded-lg border border-border bg-card px-4 py-3 flex items-start gap-4"
              >
                {/* Direction badge */}
                <span
                  className={`mt-0.5 flex-shrink-0 rounded-full px-2 py-0.5 text-xs font-medium ${dirClass}`}
                >
                  {dirLabel}
                </span>

                {/* Qty */}
                <span className="font-mono text-sm flex-shrink-0 w-24 text-right">
                  {sign}
                  {formatDecimal(m.qty_base)}
                </span>

                {/* Details */}
                <div className="flex-1 min-w-0 text-xs text-muted-foreground space-y-0.5">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                    <span>
                      单价 <span className="font-mono">{formatDecimal(m.unit_cost, 4)}</span>
                    </span>
                    <span aria-hidden="true">·</span>
                    <span>仓库 <span className="font-mono">{shortId(m.warehouse_id)}</span></span>
                    <span aria-hidden="true">·</span>
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
                        <span aria-hidden="true">·</span>
                        <span className="truncate max-w-[200px]">{m.note}</span>
                      </>
                    )}
                  </div>
                </div>

                {/* Timestamp */}
                <span className="flex-shrink-0 text-xs text-muted-foreground tabular-nums">
                  {formatDateTime(m.occurred_at)}
                </span>
              </li>
            )
          })}
        </ol>
      )}

      {movements.length >= 200 && (
        <p className="text-center text-xs text-muted-foreground">
          仅展示最近 200 条记录。如需更早的历史，请联系管理员导出。
        </p>
      )}
    </div>
  )
}
