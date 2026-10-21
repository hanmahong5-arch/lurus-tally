"use client"

import { useEffect, useState } from "react"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import Decimal from "decimal.js"
import { formatCNY } from "@/lib/format"
import type { PurchaseBillItem } from "@/lib/api/purchase"

export interface PurchaseReturnDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  items: PurchaseBillItem[]
  /** product_id -> display name; unknown ids show a neutral label, never the id. */
  productNames: Record<string, string>
  /** Bill exchange rate to CNY; empty or missing means the bill is already in CNY. */
  exchangeRate?: string
  submitting: boolean
  error: string | null
  onSubmit: (lines: { bill_item_id: string; qty: string }[], remark: string) => void
}

function num(v: string | undefined): number {
  const n = parseFloat(v ?? "")
  return Number.isFinite(n) ? n : 0
}

/** Quantity still returnable on a line, trimmed to the 4 decimals the server keeps. */
export function returnablePurchaseQty(item: PurchaseBillItem): number {
  return Math.max(0, Math.round((num(item.qty) - num(item.returned_qty)) * 10000) / 10000)
}

/**
 * Lets the clerk pick how many of each bought line goes back to the supplier. Only lines with
 * something left to return are listed; the server re-checks every quantity and the stock.
 */
export function PurchaseReturnDialog({
  open,
  onOpenChange,
  items,
  productNames,
  exchangeRate,
  submitting,
  error,
  onSubmit,
}: PurchaseReturnDialogProps) {
  const [qtys, setQtys] = useState<Record<string, string>>({})
  const [remark, setRemark] = useState("")

  useEffect(() => {
    if (open) {
      setQtys({})
      setRemark("")
    }
  }, [open])

  const rows = items.filter((it) => returnablePurchaseQty(it) > 0)
  const picked = rows
    .map((it) => ({ it, qty: num(qtys[it.id]) }))
    .filter((r) => r.qty > 0)
  const tooMany = picked.find((r) => r.qty > returnablePurchaseQty(r.it))
  const rate = (() => {
    try {
      const r = new Decimal(exchangeRate || "1")
      return r.gt(0) ? r : new Decimal(1)
    } catch {
      return new Decimal(1)
    }
  })()
  const value = picked
    .reduce((sum, r) => sum.plus(new Decimal(r.qty).times(num(r.it.unit_price)).times(rate)), new Decimal(0))
    .toNumber()

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="退货给供应商"
      description="填写这次退回给供应商的数量，确认后货会从仓库扣掉，这张单还欠供应商的钱也会相应减少。"
      className="max-w-lg"
      footer={
        <>
          <Button variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
            不退了
          </Button>
          <Button
            disabled={submitting || picked.length === 0 || !!tooMany}
            onClick={() =>
              onSubmit(
                picked.map((r) => ({ bill_item_id: r.it.id, qty: String(r.qty) })),
                remark.trim()
              )
            }
          >
            {submitting ? "处理中..." : "确认退货"}
          </Button>
        </>
      }
    >
      <div className="mt-4 space-y-3">
        {error && <ErrorBanner>{error}</ErrorBanner>}
        <ul className="space-y-3">
          {rows.map((it) => {
            const left = returnablePurchaseQty(it)
            const over = num(qtys[it.id]) > left
            const name = productNames[it.product_id] || "未知商品"
            return (
              <li key={it.id} className="space-y-1">
                <div className="flex items-baseline justify-between gap-3 text-sm">
                  <span className="font-medium">{name}</span>
                  <span className="text-muted-foreground">
                    买入 {it.qty}
                    {it.unit_name ?? ""}，最多还能退 {left}
                    {it.unit_name ?? ""}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    aria-label={`${name}退货数量`}
                    type="number"
                    inputMode="decimal"
                    min="0"
                    step="any"
                    placeholder="退几个"
                    value={qtys[it.id] ?? ""}
                    onChange={(e) => setQtys((q) => ({ ...q, [it.id]: e.target.value }))}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setQtys((q) => ({ ...q, [it.id]: String(left) }))}
                  >
                    全退
                  </Button>
                </div>
                {over && <p className="text-xs text-destructive">不能超过 {left}</p>}
              </li>
            )
          })}
        </ul>
        <div className="space-y-1.5">
          <Label htmlFor="purchase-return-remark">退货原因（选填）</Label>
          <Input
            id="purchase-return-remark"
            maxLength={200}
            placeholder="例如：质量问题、送多了"
            value={remark}
            onChange={(e) => setRemark(e.target.value)}
          />
        </div>
        {picked.length > 0 && (
          <p className="text-sm text-muted-foreground">
            本次退货金额：<span className="font-mono font-medium text-foreground">{formatCNY(value)}</span>
            （按原来买入的单价算；如果货已经卖出去了一部分，库存不够就退不了）
          </p>
        )}
      </div>
    </Modal>
  )
}
