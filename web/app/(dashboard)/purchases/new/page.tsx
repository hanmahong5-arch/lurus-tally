"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
import {
  createPurchaseBill,
  type BillLineItemInput,
} from "@/lib/api/purchase"
import {
  BillLineEditor,
  type BillLineItem,
} from "@/components/bill-line-editor"
import { ProfileGate } from "@/lib/profile"
import { CurrencySelector } from "@/components/cross-border/currency-selector"
import { RateInput } from "@/components/cross-border/rate-input"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

export default function NewPurchasePage() {
  const router = useRouter()
  const [items, setItems] = useState<BillLineItem[]>([])
  const [billDate, setBillDate] = useState(
    () => new Date().toISOString().slice(0, 10)
  )
  const [shippingFee, setShippingFee] = useState("0")
  const [taxAmount, setTaxAmount] = useState("0")
  const [remark, setRemark] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Multi-currency fields (cross_border profile, Story 9.1)
  const [currency, setCurrency] = useState("CNY")
  const [exchangeRate, setExchangeRate] = useState("1")

  const inputCls =
    "w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (items.length === 0) {
      setError("请至少添加一条商品行")
      return
    }
    const emptyLine = items.find((it) => !it.product_id || !it.qty || !it.unit_price)
    if (emptyLine) {
      setError("请填写所有商品行的商品、数量和单价")
      return
    }

    setSaving(true)
    setError(null)
    try {
      const lineItems: BillLineItemInput[] = items.map((it, idx) => ({
        product_id: it.product_id,
        unit_id: it.unit_id,
        unit_name: it.unit_name,
        line_no: idx + 1,
        qty: it.qty,
        unit_price: it.unit_price,
      }))

      const res = await createPurchaseBill(
        {
          bill_date: new Date(billDate).toISOString(),
          shipping_fee: shippingFee,
          tax_amount: taxAmount,
          remark: remark || undefined,
          currency: currency !== "CNY" ? currency : undefined,
          exchange_rate: currency !== "CNY" ? exchangeRate : undefined,
          items: lineItems,
        },
        devTenantId
      )
      router.push(`/purchases/${res.bill_id}`)
    } catch (e) {
      setError(String(e))
      setSaving(false)
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="mb-6">
        <h1 className="text-xl font-semibold">新建采购单</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          填写采购单信息，保存后将生成草稿
        </p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Header fields */}
        <div className="rounded-xl border border-border bg-card p-4 space-y-4">
          <h2 className="text-sm font-medium text-muted-foreground">基本信息</h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="space-y-1">
              <label className="text-sm font-medium">单据日期</label>
              <input
                type="date"
                className={inputCls}
                value={billDate}
                onChange={(e) => setBillDate(e.target.value)}
                required
              />
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium">备注</label>
              <input
                type="text"
                className={inputCls}
                value={remark}
                placeholder="可选"
                onChange={(e) => setRemark(e.target.value)}
              />
            </div>
          </div>

          {/* Cross-border: currency + exchange rate (cross_border profile only) */}
          <ProfileGate profiles={["cross_border", "hybrid"]}>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2 border-t border-border">
              <div className="space-y-1">
                <label className="text-sm font-medium">货币</label>
                <CurrencySelector
                  value={currency}
                  onChange={(code) => {
                    setCurrency(code)
                    if (code === "CNY") setExchangeRate("1")
                  }}
                  className={inputCls}
                  disabled={saving}
                />
              </div>
              {currency !== "CNY" && (
                <div className="space-y-1">
                  <label className="text-sm font-medium">汇率（→ CNY）</label>
                  <RateInput
                    currency={currency}
                    value={exchangeRate}
                    onChange={setExchangeRate}
                    date={billDate}
                    disabled={saving}
                    className={inputCls}
                  />
                </div>
              )}
            </div>
          </ProfileGate>
        </div>

        {/* Line items */}
        <div className="rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium text-muted-foreground mb-3">商品明细</h2>
          <BillLineEditor
            items={items}
            onChange={setItems}
            shippingFee={shippingFee}
            taxAmount={taxAmount}
            onShippingFeeChange={setShippingFee}
            onTaxAmountChange={setTaxAmount}
          />
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-3">
          <button
            type="button"
            onClick={() => router.back()}
            className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted transition-colors"
          >
            取消
          </button>
          <button
            type="submit"
            disabled={saving}
            className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-60"
          >
            {saving ? "保存中..." : "保存草稿"}
          </button>
        </div>
      </form>
    </div>
  )
}
