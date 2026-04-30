"use client"

import { useState, useEffect } from "react"
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
import { useDraft } from "@/hooks/useDraft"
import { DraftBadge } from "@/components/draft/DraftBadge"
import { DraftRestoreToast } from "@/components/draft/DraftRestoreToast"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

/** Fields persisted as a purchase bill draft. */
interface PurchaseDraft {
  items: BillLineItem[]
  billDate: string
  shippingFee: string
  taxAmount: string
  remark: string
  currency: string
  exchangeRate: string
}

const PURCHASE_INITIAL: PurchaseDraft = {
  items: [],
  billDate: new Date().toISOString().slice(0, 10),
  shippingFee: "0",
  taxAmount: "0",
  remark: "",
  currency: "CNY",
  exchangeRate: "1",
}

export default function NewPurchasePage() {
  const router = useRouter()

  const draft = useDraft<PurchaseDraft>("draft:purchase:new", PURCHASE_INITIAL)

  const [items, setItems] = useState<BillLineItem[]>(draft.value.items ?? [])
  const [billDate, setBillDate] = useState(
    draft.value.billDate ?? new Date().toISOString().slice(0, 10)
  )
  const [shippingFee, setShippingFee] = useState(draft.value.shippingFee ?? "0")
  const [taxAmount, setTaxAmount] = useState(draft.value.taxAmount ?? "0")
  const [remark, setRemark] = useState(draft.value.remark ?? "")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Multi-currency fields (cross_border profile, Story 9.1)
  const [currency, setCurrency] = useState(draft.value.currency ?? "CNY")
  const [exchangeRate, setExchangeRate] = useState(draft.value.exchangeRate ?? "1")

  // When draft is restored from IDB (restoredAt flips non-null), sync local state.
  useEffect(() => {
    if (!draft.restoredAt) return
    setItems(draft.value.items ?? [])
    setBillDate(draft.value.billDate ?? new Date().toISOString().slice(0, 10))
    setShippingFee(draft.value.shippingFee ?? "0")
    setTaxAmount(draft.value.taxAmount ?? "0")
    setRemark(draft.value.remark ?? "")
    setCurrency(draft.value.currency ?? "CNY")
    setExchangeRate(draft.value.exchangeRate ?? "1")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft.restoredAt])

  // Persist field changes to draft (debounced inside useDraft).
  useEffect(() => {
    draft.setValue({
      items,
      billDate,
      shippingFee,
      taxAmount,
      remark,
      currency,
      exchangeRate,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, billDate, shippingFee, taxAmount, remark, currency, exchangeRate])

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
      await draft.markSubmitted()
      router.push(`/purchases/${res.bill_id}`)
    } catch (e) {
      setError(String(e))
      setSaving(false)
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="mb-6">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold">新建采购单</h1>
          <DraftBadge status={draft.status} />
        </div>
        <p className="text-sm text-muted-foreground mt-0.5">
          填写采购单信息，保存后将生成草稿
        </p>
      </div>

      <DraftRestoreToast
        restoredAt={draft.restoredAt}
        onDiscard={draft.discardDraft}
      />

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
