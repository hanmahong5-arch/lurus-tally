"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { lineCents, roundCents } from "@/lib/cents"
import type { BillLineItemInput } from "@/lib/api/purchase"
import { ProductPicker } from "@/components/entity-pickers"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useLastPriceFill, type LastPriceLine } from "@/hooks/use-last-price-fill"
import { LastPriceHint, LineNumberInput, MobileOp, RemoveLineButton, lineInputClass } from "@/components/line-fields"
import { listProductUnits, type ProductUnitOption } from "@/lib/api/product-units"

export interface BillLineItem extends BillLineItemInput, Omit<LastPriceLine, keyof BillLineItemInput> {
  // Runtime-only: computed subtotal (not sent to server — server recalculates)
  _subtotal?: string
  // Runtime-only: display name of the picked product (kept in drafts so a
  // restored draft shows names, not ids).
  product_name?: string
}

export interface UnitDef {
  id: string
  name: string
  code: string
}

interface Props {
  items: BillLineItem[]
  onChange: (items: BillLineItem[]) => void
  /** @deprecated Options now come from each product's own unit list; kept for old callers. */
  units?: UnitDef[]
  shippingFee: string
  taxAmount: string
  onShippingFeeChange: (v: string) => void
  onTaxAmountChange: (v: string) => void
  readOnly?: boolean
  /** Supplier of the bill: when set, a picked product's price defaults to what was paid last time. */
  partnerId?: string
}

function computeSubtotal(qty: string, unitPrice: string): string {
  return lineCents(parseFloat(qty) || 0, parseFloat(unitPrice) || 0).toFixed(2)
}

/** The product's units; a unit already on the line stays selectable so it never disappears. */
function unitChoices(item: BillLineItem, opts: ProductUnitOption[] | undefined): { id: string; name: string }[] {
  const list: { id: string; name: string }[] = (opts ?? []).map((o) => ({ id: o.id, name: o.name }))
  if (item.unit_id && !list.some((o) => o.id === item.unit_id)) {
    list.unshift({ id: item.unit_id, name: item.unit_name || "当前单位" })
  }
  return list
}

function sumSubtotals(items: BillLineItem[]): string {
  const total = items.reduce((acc, it) => {
    return acc + (parseFloat(it._subtotal ?? "0") || 0)
  }, 0)
  return roundCents(total).toFixed(2)
}

/**
 * BillLineEditor — reusable line-item editor for purchase/sale bills.
 * Controlled component: all state lives in the parent via items + onChange.
 * Story 7.1 (sale bills) can use this component directly with identical props.
 */
export function BillLineEditor({
  items,
  onChange,
  shippingFee,
  taxAmount,
  onShippingFeeChange,
  onTaxAmountChange,
  readOnly = false,
  partnerId,
}: Props) {
  const tenantId = useTenantId()
  const { fill: fillLastPrices, patchProduct } = useLastPriceFill(items, onChange, partnerId, "purchase", tenantId, !readOnly, true)

  // Units per product: a line may only be billed in a unit the product can convert.
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
          // Allow a retry the next time the line set changes.
          requestedUnits.current.delete(pid)
        }
      )
    }
  }, [productKey, readOnly, tenantId])

  // Lines still without a unit start on their product's base unit, whenever the unit
  // list is known: a re-picked product or a second line of the same product included.
  useEffect(() => {
    if (readOnly) return
    for (const pid of Array.from(new Set(productKey.split(",").filter(Boolean)))) {
      const base = unitOptions[pid]?.find((o) => o.is_base)
      if (!base) continue
      patchProduct(pid, (it) => (it.unit_id ? null : { ...it, unit_id: base.id, unit_name: base.name }))
    }
  }, [items, productKey, unitOptions, readOnly, patchProduct])

  const addRow = useCallback(() => {
    onChange([
      ...items,
      {
        product_id: "",
        qty: "1",
        unit_price: "0",
        line_no: items.length + 1,
        _subtotal: "0",
      },
    ])
  }, [items, onChange])

  const removeRow = useCallback(
    (idx: number) => {
      onChange(items.filter((_, i) => i !== idx))
    },
    [items, onChange]
  )

  const updateRow = useCallback(
    (idx: number, patch: Partial<BillLineItem>) => {
      const next = items.map((it, i) => {
        if (i !== idx) return it
        const updated = { ...it, ...patch }
        updated._subtotal = computeSubtotal(updated.qty, updated.unit_price)
        return updated
      })
      onChange(next)
    },
    [items, onChange]
  )

  const subtotal = sumSubtotals(items)
  const totalAmount = roundCents(
    parseFloat(subtotal) + (parseFloat(shippingFee) || 0) + (parseFloat(taxAmount) || 0)
  ).toFixed(2)

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="block w-full text-sm md:table">
          <thead className="hidden bg-muted/50 text-muted-foreground md:table-header-group">
            <tr>
              <th className="px-3 py-2 text-left font-medium w-8">#</th>
              <th className="px-3 py-2 text-left font-medium">商品</th>
              <th className="px-3 py-2 text-left font-medium w-28">单位</th>
              <th className="px-3 py-2 text-right font-medium w-24">数量</th>
              <th className="px-3 py-2 text-right font-medium w-28">单价</th>
              <th className="px-3 py-2 text-right font-medium w-28">小计</th>
              {!readOnly && (
                <th className="px-3 py-2 w-10" />
              )}
            </tr>
          </thead>
          <tbody className="block divide-y divide-border md:table-row-group">
            {items.map((item, idx) => (
              <tr key={idx} className="flex flex-wrap items-start gap-x-2 gap-y-1 px-3 py-3 hover:bg-muted/20 md:table-row md:px-0 md:py-0">
                <td className="hidden px-3 py-2 text-muted-foreground md:table-cell">{idx + 1}</td>
                <td className="w-full px-0 py-0 md:table-cell md:w-auto md:px-3 md:py-2">
                  {readOnly ? (
                    <span>{item.product_name || "未知商品"}</span>
                  ) : (
                    <ProductPicker
                      value={item.product_id}
                      label={item.product_name ?? ""}
                      onSelect={(p) => {
                        updateRow(idx, {
                          product_id: p.id,
                          product_name: p.name,
                          unit_id: undefined,
                          unit_name: undefined,
                          _price_touched: false,
                          _last_price: undefined,
                          _prev_price: undefined,
                          _prev_unit: undefined,
                        })
                        fillLastPrices([p.id])
                      }}
                    />
                  )}
                </td>
                <td className="w-full px-0 py-0 md:table-cell md:w-auto md:px-3 md:py-2">
                  {readOnly ? (
                    <span>{item.unit_name || "—"}</span>
                  ) : (
                    <select
                      className="w-full rounded-md border border-input bg-transparent px-2 py-1 text-base md:text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                      value={item.unit_id ?? ""}
                      onChange={(e) => {
                        const uid = e.target.value
                        const u = unitChoices(item, unitOptions[item.product_id]).find((u) => u.id === uid)
                        const patch: Partial<BillLineItem> = { unit_id: uid, unit_name: u?.name }
                        // An auto-filled price follows the unit: per-base price x the new unit's factor.
                        const factor = parseFloat(unitOptions[item.product_id]?.find((o) => o.id === uid)?.conversion_factor ?? "")
                        const base = parseFloat(item._last_price?.base_value ?? "")
                        if (!item._price_touched && factor > 0 && base >= 0) {
                          patch.unit_price = (base * factor).toFixed(2)
                        }
                        updateRow(idx, patch)
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
                <td className="w-24 px-0 py-0 md:table-cell md:w-auto md:px-3 md:py-2">
                  <LineNumberInput
                    ariaLabel="数量"
                    value={item.qty}
                    min="0"
                    step="0.0001"
                    readOnly={readOnly}
                    onChange={(v) => updateRow(idx, { qty: v })}
                  />
                </td>
                <td className="flex-1 px-0 py-0 md:table-cell md:px-3 md:py-2">
                  <div className="flex items-center gap-1">
                    <MobileOp>×</MobileOp>
                    <LineNumberInput
                      ariaLabel="单价"
                      value={item.unit_price}
                      min="0"
                      step="0.000001"
                      readOnly={readOnly}
                      onChange={(v) => updateRow(idx, { unit_price: v, _price_touched: true })}
                    />
                  </div>
                  <LastPriceHint last={item._last_price} />
                </td>
                <td className="px-0 py-0 text-right font-mono text-base md:table-cell md:px-3 md:py-2 md:text-xs">
                  <MobileOp>=</MobileOp>
                  {item._subtotal ?? computeSubtotal(item.qty, item.unit_price)}
                </td>
                {!readOnly && (
                  <td className="px-0 py-0 text-center md:table-cell md:px-3 md:py-2">
                    <RemoveLineButton onClick={() => removeRow(idx)} />
                  </td>
                )}
              </tr>
            ))}
            {items.length === 0 && (
              <tr className="block md:table-row">
                <td
                  colSpan={readOnly ? 6 : 7}
                  className="block md:table-cell px-3 py-6 text-center text-muted-foreground text-sm"
                >
                  暂无商品行
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {!readOnly && (
        <button
          type="button"
          onClick={addRow}
          className="text-sm text-primary hover:underline"
        >
          + 添加行
        </button>
      )}

      {/* Totals */}
      <div className="flex flex-col items-end gap-1 border-t border-border pt-3 text-sm">
        <div className="flex gap-8">
          <span className="text-muted-foreground">商品合计</span>
          <span className="font-mono w-28 text-right">{subtotal}</span>
        </div>
        <div className="flex gap-8 items-center">
          <span className="text-muted-foreground">运费</span>
          {readOnly ? (
            <span className="font-mono w-28 text-right">{shippingFee}</span>
          ) : (
            <input
              type="number"
              className={`w-28 ${lineInputClass}`}
              inputMode="decimal"
              value={shippingFee}
              min="0"
              step="0.01"
              onChange={(e) => onShippingFeeChange(e.target.value)}
            />
          )}
        </div>
        <div className="flex gap-8 items-center">
          <span className="text-muted-foreground">税额</span>
          {readOnly ? (
            <span className="font-mono w-28 text-right">{taxAmount}</span>
          ) : (
            <input
              type="number"
              className={`w-28 ${lineInputClass}`}
              inputMode="decimal"
              value={taxAmount}
              min="0"
              step="0.01"
              onChange={(e) => onTaxAmountChange(e.target.value)}
            />
          )}
        </div>
        <div className="flex gap-8 border-t border-border pt-1 font-semibold">
          <span>含税总额</span>
          <span className="font-mono w-28 text-right">{totalAmount}</span>
        </div>
      </div>
    </div>
  )
}
