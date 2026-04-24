"use client"

import { useCallback } from "react"
import type { BillLineItemInput } from "@/lib/api/purchase"

export interface BillLineItem extends BillLineItemInput {
  // Runtime-only: computed subtotal (not sent to server — server recalculates)
  _subtotal?: string
}

export interface Product {
  id: string
  name: string
  code: string
}

export interface UnitDef {
  id: string
  name: string
  code: string
}

interface Props {
  items: BillLineItem[]
  onChange: (items: BillLineItem[]) => void
  products?: Product[]
  units?: UnitDef[]
  shippingFee: string
  taxAmount: string
  onShippingFeeChange: (v: string) => void
  onTaxAmountChange: (v: string) => void
  readOnly?: boolean
}

function computeSubtotal(qty: string, unitPrice: string): string {
  const q = parseFloat(qty) || 0
  const p = parseFloat(unitPrice) || 0
  return (q * p).toFixed(4)
}

function sumSubtotals(items: BillLineItem[]): string {
  const total = items.reduce((acc, it) => {
    return acc + (parseFloat(it._subtotal ?? "0") || 0)
  }, 0)
  return total.toFixed(4)
}

/**
 * BillLineEditor — reusable line-item editor for purchase/sale bills.
 * Controlled component: all state lives in the parent via items + onChange.
 * Story 7.1 (sale bills) can use this component directly with identical props.
 */
export function BillLineEditor({
  items,
  onChange,
  products = [],
  units = [],
  shippingFee,
  taxAmount,
  onShippingFeeChange,
  onTaxAmountChange,
  readOnly = false,
}: Props) {
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
  const totalAmount = (
    parseFloat(subtotal) +
    (parseFloat(shippingFee) || 0) +
    (parseFloat(taxAmount) || 0)
  ).toFixed(4)

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-muted-foreground">
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
          <tbody className="divide-y divide-border">
            {items.map((item, idx) => (
              <tr key={idx} className="hover:bg-muted/20">
                <td className="px-3 py-2 text-muted-foreground">{idx + 1}</td>
                <td className="px-3 py-2">
                  {readOnly ? (
                    <span>{item.product_id}</span>
                  ) : (
                    <select
                      className="w-full rounded-md border border-input bg-background px-2 py-1 text-sm outline-none focus:ring-1 focus:ring-ring"
                      value={item.product_id}
                      onChange={(e) => updateRow(idx, { product_id: e.target.value })}
                    >
                      <option value="">请选择商品</option>
                      {products.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name} ({p.code})
                        </option>
                      ))}
                    </select>
                  )}
                </td>
                <td className="px-3 py-2">
                  {readOnly ? (
                    <span>{item.unit_name ?? item.unit_id ?? "—"}</span>
                  ) : (
                    <select
                      className="w-full rounded-md border border-input bg-background px-2 py-1 text-sm outline-none focus:ring-1 focus:ring-ring"
                      value={item.unit_id ?? ""}
                      onChange={(e) => {
                        const uid = e.target.value
                        const u = units.find((u) => u.id === uid)
                        updateRow(idx, { unit_id: uid, unit_name: u?.name })
                      }}
                    >
                      <option value="">—</option>
                      {units.map((u) => (
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
                    className="w-full rounded-md border border-input bg-background px-2 py-1 text-sm text-right outline-none focus:ring-1 focus:ring-ring"
                    value={item.qty}
                    min="0"
                    step="0.0001"
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
                    step="0.000001"
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
                  colSpan={readOnly ? 6 : 7}
                  className="px-3 py-6 text-center text-muted-foreground text-sm"
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
              className="w-28 rounded-md border border-input bg-background px-2 py-1 text-sm text-right outline-none focus:ring-1 focus:ring-ring"
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
              className="w-28 rounded-md border border-input bg-background px-2 py-1 text-sm text-right outline-none focus:ring-1 focus:ring-ring"
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
