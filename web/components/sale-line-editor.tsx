"use client"

/**
 * SaleLineEditor — sale-bill-specific wrapper around BillLineEditor.
 * Omits shipping fee and tax amount fields (retail sales don't need them).
 * Displays a running total for POS/quick-checkout UX.
 */

import { useCallback } from "react"
import Decimal from "decimal.js"
import type { BillLineItemInput } from "@/lib/api/purchase"
import { formatCNY } from "@/lib/format"
import { lineCents, roundCents } from "@/lib/cents"
import { ProductPicker } from "@/components/entity-pickers"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useLastPriceFill, type LastPriceLine } from "@/hooks/use-last-price-fill"
import { LastPriceHint, LineNumberInput, MobileOp, RemoveLineButton } from "@/components/line-fields"

export interface SaleLineItem extends BillLineItemInput, Omit<LastPriceLine, keyof BillLineItemInput> {
  _subtotal?: string
  // Runtime-only: display name of the picked product.
  product_name?: string
}

interface Props {
  items: SaleLineItem[]
  onChange: (items: SaleLineItem[]) => void
  readOnly?: boolean
  /** Customer of the bill: when set, a picked product's price defaults to what they paid last time. */
  partnerId?: string
}

function computeSubtotal(qty: string, unitPrice: string): string {
  return lineCents(parseFloat(qty) || 0, parseFloat(unitPrice) || 0).toFixed(2)
}

function sumSubtotals(items: SaleLineItem[]): number {
  return roundCents(items.reduce((acc, it) => acc + (parseFloat(it._subtotal ?? "0") || 0), 0))
}

/** Half-up to the cent on the decimal string, so 2.675 does not become 2.67 through float noise. */
function roundToCent(price: string): string {
  try {
    return new Decimal(price).toDecimalPlaces(2, Decimal.ROUND_HALF_UP).toFixed(2)
  } catch {
    return price
  }
}

/**
 * A filled-in price converted to the base unit can carry many decimals (3.333333). The till
 * charges in cents, so an auto-filled price is rounded; a price the clerk typed is left alone.
 */
function roundFilledPrices(lines: SaleLineItem[]): SaleLineItem[] {
  return lines.map((it) => {
    if (!it._last_price || it._price_touched) return it
    const price = roundToCent(it.unit_price)
    if (price === it.unit_price) return it
    return { ...it, unit_price: price, _subtotal: computeSubtotal(it.qty, price) }
  })
}

export function SaleLineEditor({ items, onChange, readOnly = false, partnerId }: Props) {
  const tenantId = useTenantId()
  const onLinesChange = useCallback((next: SaleLineItem[]) => onChange(roundFilledPrices(next)), [onChange])
  const { fill: fillLastPrices } = useLastPriceFill(items, onLinesChange, partnerId, "sale", tenantId, !readOnly)

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
    (idx: number, patch: Partial<SaleLineItem>) => {
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

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="block w-full text-sm md:table">
          <thead className="hidden bg-muted/50 text-muted-foreground md:table-header-group">
            <tr>
              <th className="px-3 py-2 text-left font-medium w-8">#</th>
              <th className="px-3 py-2 text-left font-medium">商品</th>
              <th className="px-3 py-2 text-right font-medium w-24">数量</th>
              <th className="px-3 py-2 text-right font-medium w-28">售价</th>
              <th className="px-3 py-2 text-right font-medium w-28">小计</th>
              {!readOnly && <th className="px-3 py-2 w-10" />}
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
                          // A copied line carries the old product's unit; this editor has no unit
                          // column to fix it, so drop it and bill the new product in its base unit.
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
                <td className="w-24 px-0 py-0 md:table-cell md:w-auto md:px-3 md:py-2">
                  <LineNumberInput
                    ariaLabel="数量"
                    value={item.qty}
                    min="0.0001"
                    step="1"
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
                      step="0.01"
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
                  colSpan={readOnly ? 5 : 6}
                  className="block md:table-cell px-3 py-6 text-center text-muted-foreground text-sm"
                >
                  暂无商品行，请添加
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

      {/* Running total */}
      <div className="flex justify-end">
        <div className="flex gap-8 border-t border-border pt-2 font-semibold text-sm">
          <span>合计</span>
          <span className="font-mono w-28 text-right tabular-nums">{formatCNY(sumSubtotals(items))}</span>
        </div>
      </div>
    </div>
  )
}
