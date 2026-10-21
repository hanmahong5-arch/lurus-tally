"use client"

import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { ApiError, extractApiError } from "@/lib/api/errors"
import { adjustStock, type StockAdjustItem, type StockSnapshot } from "@/lib/api/stock"
import { listProducts, type Product } from "@/lib/api/products"
import { BarcodeScanner } from "@/components/barcode-scanner"
import { useRole } from "@/hooks/use-role"

export interface CountWarehouse {
  id: string
  name: string
  isDefault?: boolean
}

export interface StockAdjustDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  tenantId?: string
  products: Product[]
  snapshots: StockSnapshot[]
  warehouses: CountWarehouse[]
  /** Called after a count was saved (to reload stock). */
  onDone: () => void
}

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

/** At most this many matching products are listed at once; searching narrows the list. */
const MAX_ROWS = 100

function toNumber(raw: string): number {
  return Number(raw.trim())
}

/** A counted value is usable when it is a non-negative number. */
export function isValidCount(raw: string): boolean {
  const t = raw.trim()
  if (t === "") return false
  const n = Number(t)
  return Number.isFinite(n) && n >= 0
}

/** A purchase price is usable when it is a number above zero. */
export function isValidCost(raw: string): boolean {
  const t = raw.trim()
  if (t === "") return false
  const n = Number(t)
  return Number.isFinite(n) && n > 0
}

/**
 * Products the server could not value (empty book, no purchase price on file): the answer to a
 * count it refused with code cost_required. Empty for any other error.
 */
export function costRequiredIds(e: unknown): string[] {
  if (!(e instanceof ApiError)) return []
  const body = (e.body ?? {}) as { code?: unknown; product_ids?: unknown }
  if (body.code !== "cost_required" || !Array.isArray(body.product_ids)) return []
  return body.product_ids.filter((x): x is string => typeof x === "string")
}

/** Product names the server attached to a cost_required refusal; empty when it sent none. */
export function costRequiredNames(e: unknown): string[] {
  if (!(e instanceof ApiError)) return []
  const body = (e.body ?? {}) as { code?: unknown; product_names?: unknown }
  if (body.code !== "cost_required" || !Array.isArray(body.product_names)) return []
  return body.product_names.filter((x): x is string => typeof x === "string" && x.trim() !== "")
}

/** Id and name pairs the server attached to a cost_required refusal; empty when it sent none. */
export function costRequiredProducts(e: unknown): { id: string; name: string }[] {
  if (!(e instanceof ApiError)) return []
  const body = (e.body ?? {}) as { code?: unknown; products?: unknown }
  if (body.code !== "cost_required" || !Array.isArray(body.products)) return []
  const out: { id: string; name: string }[] = []
  for (const x of body.products) {
    const r = (x ?? {}) as { id?: unknown; name?: unknown }
    if (typeof r.id === "string" && typeof r.name === "string" && r.name.trim() !== "") {
      out.push({ id: r.id, name: r.name })
    }
  }
  return out
}

/** Trims float noise (0.1 + 0.2) so differences read like what a person would count. */
export function formatQty(n: number): string {
  return String(Math.round(n * 10000) / 10000)
}

/**
 * Stock count ("盘点"): the clerk types the quantity actually on the shelf for the products
 * they counted; blank rows are left alone. Only differences are written, as adjustments
 * (the average cost is not touched), and no bill is created.
 */
export function StockAdjustDialog({
  open,
  onOpenChange,
  tenantId,
  products,
  snapshots,
  warehouses,
  onDone,
}: StockAdjustDialogProps) {
  const [warehouseId, setWarehouseId] = useState("")
  const [q, setQ] = useState("")
  const [counts, setCounts] = useState<Record<string, string>>({})
  const [remark, setRemark] = useState("")
  // Purchase price per product, asked for only where the server could not value the stock.
  const [costs, setCosts] = useState<Record<string, string>>({})
  const [needCost, setNeedCost] = useState<string[]>([])
  // Only people who may see purchase prices are asked for one; the backend drops it from anyone else.
  const { can } = useRole()
  const canCost = can("cost.view")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setCounts({})
    setCosts({})
    setNeedCost([])
    setQ("")
    setRemark("")
    setError(null)
    setWarehouseId((cur) => {
      if (cur && warehouses.some((w) => w.id === cur)) return cur
      return (warehouses.find((w) => w.isDefault) ?? warehouses[0])?.id ?? ""
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const bookQty = useMemo(() => {
    const m = new Map<string, number>()
    for (const s of snapshots) {
      if (s.warehouse_id === warehouseId) m.set(s.product_id, toNumber(s.on_hand_qty))
    }
    return m
  }, [snapshots, warehouseId])

  // Server-side search: the preloaded list only holds the first few hundred products,
  // a shop that imported thousands must still be able to find any of them.
  const [found, setFound] = useState<{ q: string; items: Product[] } | null>(null)
  useEffect(() => {
    const needle = q.trim()
    if (!open || !needle) return
    const ctl = new AbortController()
    const timer = setTimeout(() => {
      listProducts({ q: needle, limit: MAX_ROWS, tenantId, signal: ctl.signal })
        .then((res) => setFound({ q: needle, items: res.items ?? [] }))
        .catch(() => {
          // keep the local filter below as the fallback
        })
    }, 300)
    return () => {
      clearTimeout(timer)
      ctl.abort()
    }
  }, [q, open, tenantId])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    if (needle && found && found.q === q.trim()) return found.items.slice(0, MAX_ROWS)
    const list = products.filter((p) => {
      if (!needle) return true
      return `${p.name} ${p.code} ${p.mnemonic ?? ""}`.toLowerCase().includes(needle)
    })
    return list.slice(0, MAX_ROWS)
  }, [products, q, found])

  const entered = Object.entries(counts).filter(([, v]) => v.trim() !== "")
  const invalid = entered.filter(([, v]) => !isValidCount(v))
  // A product the server asked a price for cannot be sent without one.
  const missingCost = entered.filter(([id]) => needCost.includes(id) && !isValidCost(costs[id] ?? ""))
  const changed = entered.filter(([id, v]) => isValidCount(v) && Number(v) !== (bookQty.get(id) ?? 0))

  function changeWarehouse(id: string) {
    setWarehouseId(id)
    setCounts({}) // book quantities differ per warehouse; typed counts no longer apply
    setCosts({})
    setNeedCost([])
  }

  async function submit() {
    setSubmitting(true)
    setError(null)
    try {
      const res = await adjustStock(
        {
          warehouse_id: warehouseId,
          remark: remark.trim() || undefined,
          items: entered.map(([product_id, v]): StockAdjustItem & { unit_cost?: string } => ({
            product_id,
            counted_qty: v.trim(),
            ...(isValidCost(costs[product_id] ?? "") ? { unit_cost: costs[product_id].trim() } : {}),
          })),
        },
        tenantId
      )
      toast.success(
        res.adjusted > 0
          ? `盘点完成：${res.adjusted} 个商品的库存已按实际数量调整`
          : "盘点完成：数量都对得上，库存没有变化"
      )
      onDone()
      onOpenChange(false)
    } catch (e) {
      const ids = costRequiredIds(e)
      if (ids.length > 0 && !canCost) {
        // Not theirs to price: list the products by name so the owner knows what to fill in.
        const byId = new Map(products.map((p) => [p.id, p.name]))
        // Ids the loaded list does not know (a product found by search only) take the name the server sent for that id.
        for (const r of costRequiredProducts(e)) if (!byId.has(r.id)) byId.set(r.id, r.name)
        const names = ids.map((id) => byId.get(id)).filter((n): n is string => !!n)
        // An older server sends bare names only; use them when ids could not all be resolved.
        if (names.length < ids.length) {
          const left = costRequiredNames(e)
          if (left.length > names.length) names.splice(0, names.length, ...left)
        }
        setError(
          `这些商品还没有进价，请老板或店长先填进价，或由他们来盘点${names.length > 0 ? "：" + names.join("、") : ""}。其他商品可以先把这几行清空，再确认盘点。`
        )
      } else if (ids.length > 0) {
        setNeedCost((cur) => Array.from(new Set([...cur, ...ids])))
        setError("有商品账上是 0 又没有进价记录，请在标出的商品后面填上每件的进货价，再确认盘点。")
      } else {
        setError(extractApiError(e).message)
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={(o) => {
        if (!submitting) onOpenChange(o)
      }}
      title="盘点库存"
      description={
        canCost
          ? "在要盘点的商品后面填上货架上实际数的数量，没填的商品不会动。确认后库存会改成你填的数量；原来有库存的，成本价不变，账上是 0 的按商品的进货价入账。"
          : "在要盘点的商品后面填上货架上实际数的数量，没填的商品不会动。确认后库存会改成你填的数量。"
      }
      className="max-w-2xl"
      footer={
        <>
          <Button variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={submitting || !warehouseId || entered.length === 0 || invalid.length > 0 || missingCost.length > 0}
            onClick={submit}
          >
            {submitting
              ? "保存中..."
              : changed.length > 0
                ? `确认盘点（${changed.length} 个有差异）`
                : "确认盘点"}
          </Button>
        </>
      }
    >
      <div className="mt-4 space-y-3 text-sm">
        {error && <ErrorBanner>{error}</ErrorBanner>}

        {warehouses.length === 0 ? (
          <p className="text-muted-foreground">还没有仓库，请先到「仓库」页面添加一个仓库。</p>
        ) : (
          <>
            <div className="flex flex-wrap items-end gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="count-warehouse">盘点哪个仓库</Label>
                <select
                  id="count-warehouse"
                  className={SELECT_CLASS}
                  value={warehouseId}
                  onChange={(e) => changeWarehouse(e.target.value)}
                >
                  {warehouses.map((w) => (
                    <option key={w.id} value={w.id}>
                      {w.name}
                      {w.isDefault ? "（默认）" : ""}
                    </option>
                  ))}
                </select>
              </div>
              <Input
                aria-label="搜索要盘点的商品"
                className="min-w-[10rem] flex-1"
                placeholder="搜索商品名称、编码..."
                value={q}
                onChange={(e) => setQ(e.target.value)}
              />
              <BarcodeScanner onScan={(code) => setQ(code)} />
            </div>

            {shown.length === 0 ? (
              <p className="text-muted-foreground">没有找到商品。</p>
            ) : (
              <ul className="max-h-72 divide-y overflow-y-auto rounded-lg border">
                {shown.map((p) => {
                  const book = bookQty.get(p.id) ?? 0
                  const raw = counts[p.id] ?? ""
                  const ok = isValidCount(raw)
                  const diff = ok ? Number(raw) - book : 0
                  return (
                    <li key={p.id} className="flex items-center gap-3 px-3 py-2">
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium">{p.name}</div>
                        <div className="text-xs text-muted-foreground">账上库存 {formatQty(book)}</div>
                      </div>
                      {ok && diff !== 0 && (
                        <span className={diff > 0 ? "text-xs text-success" : "text-xs text-destructive"}>
                          {diff > 0 ? `多了 ${formatQty(diff)}` : `少了 ${formatQty(-diff)}`}
                        </span>
                      )}
                      {raw.trim() !== "" && !ok && <span className="text-xs text-destructive">请填 0 或以上的数字</span>}
                      {needCost.includes(p.id) && ok && (
                        <Input
                          aria-label={`${p.name}进货价`}
                          className="w-28 border-destructive"
                          type="number"
                          inputMode="decimal"
                          min="0"
                          step="any"
                          placeholder="每件进货价"
                          value={costs[p.id] ?? ""}
                          onChange={(e) => setCosts((c) => ({ ...c, [p.id]: e.target.value }))}
                        />
                      )}
                      <Input
                        aria-label={`${p.name}实际数量`}
                        className="w-28"
                        type="number"
                        inputMode="decimal"
                        min="0"
                        step="any"
                        placeholder="实际数量"
                        value={raw}
                        onChange={(e) => setCounts((c) => ({ ...c, [p.id]: e.target.value }))}
                      />
                    </li>
                  )
                })}
              </ul>
            )}
            {shown.length === MAX_ROWS && (
              <p className="text-xs text-muted-foreground">只显示前 {MAX_ROWS} 个商品，输入名称可以缩小范围。</p>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="count-remark">备注（选填）</Label>
              <Input
                id="count-remark"
                maxLength={200}
                placeholder="例如：月底盘点"
                value={remark}
                onChange={(e) => setRemark(e.target.value)}
              />
            </div>
          </>
        )}
      </div>
    </Modal>
  )
}
