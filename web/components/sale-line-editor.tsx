"use client"

/**
 * SaleLineEditor — sale-bill-specific wrapper around BillLineEditor.
 * Omits shipping fee and tax amount fields (retail sales don't need them).
 * Displays a running total for POS/quick-checkout UX.
 */

import { useCallback } from "react"
import type { BillLineItemInput } from "@/lib/api/purchase"

export interface SaleLineItem extends BillLineItemInput {
  _subtotal?: string
}

interface Props {
  items: SaleLineItem[]
  onChange: (items: SaleLineItem[]) => void
  readOnly?: boolean
}

function computeSubtotal(qty: string, unitPrice: string): string {
  const q = parseFloat(qty) || 0
  const p = parseFloat(unitPrice) || 0
  return (q * p).toFixed(4)
}

function sumSubtotals(items: SaleLineItem[]): number {
  return items.reduce((acc, it) => acc + (parseFloat(it._subtotal ?? "0") || 0), 0)
}

export function SaleLineEditor({ items, onChange, readOnly = false }: Props) {
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

  const totalAmount = sumSubtotals(items).toFixed(2)

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-muted-foreground">
            <tr>
              <th className="px-3 py-2 text-left font-medium w-8">#</th>
              <th className="px-3 py-2 text-left font-medium">商品编号</th>
              <th className="px-3 py-2 text-right font-medium w-24">数量</th>
              <th className="px-3 py-2 text-right font-medium w-28">售价</th>
              <th className="px-3 py-2 text-right font-medium w-28">小计</th>
              {!readOnly && <th className="px-3 py-2 w-10" />}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {items.map((item, idx) => (
              <tr key={idx} className="hover:bg-muted/20">
                <td className="px-3 py-2 text-muted-foreground">{idx + 1}</td>
                <td className="px-3 py-2">
                  {readOnly ? (
                    <span className="font-mono text-xs">{item.product_id}</span>
                  ) : (
                    <input
                      type="text"
                      className="w-full rounded-md border border-input bg-background px-2 py-1 text-sm outline-none focus:ring-1 focus:ring-ring"
                      value={item.product_id}
                      placeholder="商品 ID"
                      onChange={(e) => updateRow(idx, { product_id: e.target.value })}
                    />
                  )}
                </td>
                <td className="px-3 py-2">
                  <input
                    type="number"
                    className="w-full rounded-md border border-input bg-background px-2 py-1 text-sm text-right outline-none focus:ring-1 focus:ring-ring"
                    value={item.qty}
                    min="0.0001"
                    step="1"
                    readOnly={readOnly}
                    onChange={(e) => updateRow(idx, { qty: e.target.value })}
                  />
                </td>
                <td className="px-3 py-2">
                  <input
                    type="number"
                    className="w-full rounded-md border border-input bg-background px-2 py-1 text-sm text-right outline-none focus:ring-1 focus:ring-ring"
                    value={item.unit_price}
                    min="0"
                    step="0.01"
                    readOnly={readOnly}
                    onChange={(e) => updateRow(idx, { unit_price: e.target.value })}
                  />
                </td>
                <td className="px-3 py-2 text-right font-mono text-xs">
                  {item._subtotal ?? computeSubtotal(item.qty, item.unit_price)}
                </td>
                {!readOnly && (
                  <td className="px-3 py-2 text-center">
                    <button
                      type="button"
                      className="text-destructive hover:text-destructive/70 text-xs"
                      onClick={() => removeRow(idx)}
                    >
                      ✕
                    </button>
                  </td>
                )}
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td
                  colSpan={readOnly ? 5 : 6}
                  className="px-3 py-6 text-center text-muted-foreground text-sm"
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
          <span className="font-mono w-28 text-right tabular-nums">¥ {totalAmount}</span>
        </div>
      </div>
    </div>
  )
}
