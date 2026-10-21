"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { ProductPicker } from "@/components/entity-pickers"
import { useTenantId } from "@/hooks/use-tenant-id"
import { listProductUnits, type ProductUnitOption } from "@/lib/api/product-units"
import type { BillLineItem } from "@/components/bill-line-editor"

interface Props {
  items: BillLineItem[]
  onChange: (items: BillLineItem[]) => void
  readOnly?: boolean
}

const CONTROL =
  "w-full rounded-md border border-input bg-transparent px-2 py-1 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

/** The product's units; a unit already on the line stays selectable so it never disappears. */
function unitChoices(item: BillLineItem, opts: ProductUnitOption[] | undefined): { id: string; name: string }[] {
  const list: { id: string; name: string }[] = (opts ?? []).map((o) => ({ id: o.id, name: o.name }))
  if (item.unit_id && !list.some((o) => o.id === item.unit_id)) {
    list.unshift({ id: item.unit_id, name: item.unit_name || "当前单位" })
  }
  return list
}

/**
 * PurchaseDraftLines — product and quantity lines for someone who cannot see
 * purchase prices (a clerk). No price, subtotal, freight or tax is drawn or
 * collected; the boss or manager fills the prices in before approving.
 */
export function PurchaseDraftLines({ items, onChange, readOnly = false }: Props) {
  const tenantId = useTenantId()
  const [unitOptions, setUnitOptions] = useState<Record<string, ProductUnitOption[]>>({})
  const requestedUnits = useRef(new Set<string>())

  const productKey = items.map((it) => it.product_id).filter(Boolean).join(",")
  useEffect(() => {
    if (readOnly) return
    for (const pid of Array.from(new Set(productKey.split(",").filter(Boolean)))) {
      if (requestedUnits.current.has(pid)) continue
      requestedUnits.current.add(pid)
      listProductUnits(pid, { tenantId }).then(
        (opts) => setUnitOptions((prev) => ({ ...prev, [pid]: opts })),
        () => {
          requestedUnits.current.delete(pid)
        }
      )
    }
  }, [productKey, readOnly, tenantId])

  // A line without a unit starts on its product's base unit once the list is known.
  useEffect(() => {
    if (readOnly) return
    let changed = false
    const next = items.map((it) => {
      if (it.unit_id || !it.product_id) return it
      const base = unitOptions[it.product_id]?.find((o) => o.is_base)
      if (!base) return it
      changed = true
      return { ...it, unit_id: base.id, unit_name: base.name }
    })
    if (changed) onChange(next)
  }, [items, unitOptions, readOnly, onChange])

  const addRow = useCallback(() => {
    onChange([...items, { product_id: "", qty: "1", unit_price: "0", line_no: items.length + 1 }])
  }, [items, onChange])

  const updateRow = useCallback(
    (idx: number, patch: Partial<BillLineItem>) => {
      onChange(items.map((it, i) => (i === idx ? { ...it, ...patch } : it)))
    },
    [items, onChange]
  )

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-muted-foreground">
            <tr>
              <th className="w-8 px-3 py-2 text-left font-medium">#</th>
              <th className="px-3 py-2 text-left font-medium">商品</th>
              <th className="w-28 px-3 py-2 text-left font-medium">单位</th>
              <th className="w-24 px-3 py-2 text-right font-medium">数量</th>
              {!readOnly && <th className="w-10 px-3 py-2" />}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {items.map((item, idx) => (
              <tr key={idx} className="hover:bg-muted/20">
                <td className="px-3 py-2 text-muted-foreground">{idx + 1}</td>
                <td className="px-3 py-2">
                  {readOnly ? (
                    <span>{item.product_name || "未知商品"}</span>
                  ) : (
                    <ProductPicker
                      value={item.product_id}
                      label={item.product_name ?? ""}
                      onSelect={(p) =>
                        updateRow(idx, { product_id: p.id, product_name: p.name, unit_id: undefined, unit_name: undefined })
                      }
                    />
                  )}
                </td>
                <td className="px-3 py-2">
                  {readOnly ? (
                    <span>{item.unit_name || "—"}</span>
                  ) : (
                    <select
                      className={CONTROL}
                      aria-label="单位"
                      value={item.unit_id ?? ""}
                      onChange={(e) => {
                        const u = unitChoices(item, unitOptions[item.product_id]).find((x) => x.id === e.target.value)
                        updateRow(idx, { unit_id: e.target.value, unit_name: u?.name })
                      }}
                    >
                      <option value="">—</option>
                      {unitChoices(item, unitOptions[item.product_id]).map((u) => (
                        <option key={u.id} value={u.id}>
                          {u.name}
                        </option>
                      ))}
                    </select>
                  )}
                </td>
                <td className="px-3 py-2">
                  <input
                    type="number"
                    aria-label="数量"
                    className={`${CONTROL} text-right`}
                    value={item.qty}
                    min="0"
                    step="0.0001"
                    readOnly={readOnly}
                    onChange={(e) => updateRow(idx, { qty: e.target.value })}
                  />
                </td>
                {!readOnly && (
                  <td className="px-3 py-2 text-center">
                    <button
                      type="button"
                      className="text-xs text-destructive hover:text-destructive/70"
                      onClick={() => onChange(items.filter((_, i) => i !== idx))}
                    >
                      ✕
                    </button>
                  </td>
                )}
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={readOnly ? 4 : 5} className="px-3 py-6 text-center text-sm text-muted-foreground">
                  暂无商品行
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {!readOnly && (
        <button type="button" onClick={addRow} className="text-sm text-primary hover:underline">
          + 添加行
        </button>
      )}
    </div>
  )
}
