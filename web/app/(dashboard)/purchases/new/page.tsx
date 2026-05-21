"use client"

import { useState, useEffect } from "react"
import { useRouter } from "next/navigation"
import { createPurchaseBill, type BillLineItemInput } from "@/lib/api/purchase"
import { BillLineEditor, type BillLineItem } from "@/components/bill-line-editor"
import { ProfileGate } from "@/lib/profile"
import { CurrencySelector } from "@/components/cross-border/currency-selector"
import { RateInput } from "@/components/cross-border/rate-input"
import { useDraft } from "@/hooks/useDraft"
import { DraftBadge } from "@/components/draft/DraftBadge"
import { DraftRestoreToast } from "@/components/draft/DraftRestoreToast"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const CONTROL_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"

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
  const [billDate, setBillDate] = useState(draft.value.billDate ?? new Date().toISOString().slice(0, 10))
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
    draft.setValue({ items, billDate, shippingFee, taxAmount, remark, currency, exchangeRate })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, billDate, shippingFee, taxAmount, remark, currency, exchangeRate])

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
    <PageContainer width="wide">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            新建采购单
            <DraftBadge status={draft.status} />
          </span>
        }
        subtitle="填写采购单信息，保存后将生成草稿"
      />

      <DraftRestoreToast restoredAt={draft.restoredAt} onDiscard={draft.discardDraft} />

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Header fields */}
        <div className="space-y-4 rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium text-muted-foreground">基本信息</h2>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="bill-date">单据日期</Label>
              <Input
                id="bill-date"
                type="date"
                value={billDate}
                onChange={(e) => setBillDate(e.target.value)}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="bill-remark">备注</Label>
              <Input
                id="bill-remark"
                type="text"
                value={remark}
                placeholder="可选"
                onChange={(e) => setRemark(e.target.value)}
              />
            </div>
          </div>

          {/* Cross-border: currency + exchange rate */}
          <ProfileGate profiles={["cross_border", "hybrid"]}>
            <div className="grid grid-cols-1 gap-4 border-t border-border pt-2 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>货币</Label>
                <CurrencySelector
                  value={currency}
                  onChange={(code) => {
                    setCurrency(code)
                    if (code === "CNY") setExchangeRate("1")
                  }}
                  className={CONTROL_CLASS}
                  disabled={saving}
                />
              </div>
              {currency !== "CNY" && (
                <div className="space-y-1.5">
                  <Label>汇率（→ CNY）</Label>
                  <RateInput
                    currency={currency}
                    value={exchangeRate}
                    onChange={setExchangeRate}
                    date={billDate}
                    disabled={saving}
                    className={CONTROL_CLASS}
                  />
                </div>
              )}
            </div>
          </ProfileGate>
        </div>

        {/* Line items */}
        <div className="rounded-xl border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">商品明细</h2>
          <BillLineEditor
            items={items}
            onChange={setItems}
            shippingFee={shippingFee}
            taxAmount={taxAmount}
            onShippingFeeChange={setShippingFee}
            onTaxAmountChange={setTaxAmount}
          />
        </div>

        {error && <ErrorBanner>{error}</ErrorBanner>}

        <div className="flex justify-end gap-3">
          <Button type="button" variant="outline" onClick={() => router.back()} disabled={saving}>
            取消
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? "保存中..." : "保存草稿"}
          </Button>
        </div>
      </form>
    </PageContainer>
  )
}
