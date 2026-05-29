"use client"

import { useState, useEffect, Suspense } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import {
  createSaleBill,
  quickCheckout,
  type BillLineItemInput,
} from "@/lib/api/sale"
import { SaleLineEditor, type SaleLineItem } from "@/components/sale-line-editor"
import { ProfileGate, useProfile } from "@/lib/profile"
import { CurrencySelector } from "@/components/cross-border/currency-selector"
import { RateInput } from "@/components/cross-border/rate-input"
import { useDraft } from "@/hooks/useDraft"
import { useTenantId } from "@/hooks/use-tenant-id"
import { DraftBadge } from "@/components/draft/DraftBadge"
import { DraftRestoreToast } from "@/components/draft/DraftRestoreToast"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { formatCNY } from "@/lib/format"

const CONTROL_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"

const PAY_METHODS = [
  { value: "cash", label: "现金" },
  { value: "wechat", label: "微信" },
  { value: "alipay", label: "支付宝" },
  { value: "card", label: "银行卡" },
  { value: "transfer", label: "转账" },
]

/** Fields persisted as a sale draft. */
interface SaleDraft {
  isQuick: boolean
  items: SaleLineItem[]
  billDate: string
  customerName: string
  remark: string
  paymentMethod: string
  paidAmount: string
  currency: string
  exchangeRate: string
}

function NewSaleInner() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const { profileType } = useProfile()
  const tenantId = useTenantId()

  // retail profile defaults to quick checkout; explicit ?mode=quick also triggers it
  const defaultQuick = profileType === "retail" || searchParams.get("mode") === "quick"

  const SALE_INITIAL: SaleDraft = {
    isQuick: defaultQuick,
    items: [],
    billDate: new Date().toISOString().slice(0, 10),
    customerName: "",
    remark: "",
    paymentMethod: "cash",
    paidAmount: "",
    currency: "CNY",
    exchangeRate: "1",
  }

  const draft = useDraft<SaleDraft>("draft:sale:new", SALE_INITIAL)

  const [isQuick, setIsQuick] = useState(draft.value.isQuick ?? defaultQuick)
  const [items, setItems] = useState<SaleLineItem[]>(draft.value.items ?? [])
  const [billDate, setBillDate] = useState(draft.value.billDate ?? new Date().toISOString().slice(0, 10))
  const [customerName, setCustomerName] = useState(draft.value.customerName ?? "")
  const [remark, setRemark] = useState(draft.value.remark ?? "")
  const [paymentMethod, setPaymentMethod] = useState(draft.value.paymentMethod ?? "cash")
  const [paidAmount, setPaidAmount] = useState(draft.value.paidAmount ?? "")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Multi-currency fields (cross_border profile, Story 9.1)
  const [currency, setCurrency] = useState(draft.value.currency ?? "CNY")
  const [exchangeRate, setExchangeRate] = useState(draft.value.exchangeRate ?? "1")

  // When draft is restored from IDB (restoredAt flips non-null), sync local state.
  useEffect(() => {
    if (!draft.restoredAt) return
    setIsQuick(draft.value.isQuick ?? defaultQuick)
    setItems(draft.value.items ?? [])
    setBillDate(draft.value.billDate ?? new Date().toISOString().slice(0, 10))
    setCustomerName(draft.value.customerName ?? "")
    setRemark(draft.value.remark ?? "")
    setPaymentMethod(draft.value.paymentMethod ?? "cash")
    setPaidAmount(draft.value.paidAmount ?? "")
    setCurrency(draft.value.currency ?? "CNY")
    setExchangeRate(draft.value.exchangeRate ?? "1")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft.restoredAt])

  // Persist field changes to draft (debounced inside useDraft).
  useEffect(() => {
    draft.setValue({
      isQuick,
      items,
      billDate,
      customerName,
      remark,
      paymentMethod,
      paidAmount,
      currency,
      exchangeRate,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isQuick, items, billDate, customerName, remark, paymentMethod, paidAmount, currency, exchangeRate])

  const totalAmount = items.reduce((acc, it) => {
    const qty = parseFloat(it.qty) || 0
    const price = parseFloat(it.unit_price) || 0
    return acc + qty * price
  }, 0)

  function buildLineItems(): BillLineItemInput[] {
    return items.map((it, idx) => ({
      product_id: it.product_id,
      unit_id: it.unit_id,
      unit_name: it.unit_name,
      line_no: idx + 1,
      qty: it.qty,
      unit_price: it.unit_price,
    }))
  }

  async function handleQuickCheckout(e: React.FormEvent) {
    e.preventDefault()
    if (items.length === 0) {
      setError("请至少添加一条商品行")
      return
    }
    const emptyLine = items.find((it) => !it.product_id || !it.qty || !it.unit_price)
    if (emptyLine) {
      setError("请填写所有商品行的商品编号、数量和售价")
      return
    }
    const paid = parseFloat(paidAmount) || 0
    if (paid <= 0) {
      setError("请输入实收金额")
      return
    }

    setSaving(true)
    setError(null)
    try {
      const res = await quickCheckout(
        {
          customer_name: customerName || undefined,
          items: buildLineItems(),
          payment_method: paymentMethod,
          paid_amount: String(paid),
          remark: remark || undefined,
        },
        tenantId
      )
      await draft.markSubmitted()
      router.push(`/sales/${res.bill_id}`)
    } catch (err) {
      setError(String(err))
      setSaving(false)
    }
  }

  async function handleCreateDraft(e: React.FormEvent) {
    e.preventDefault()
    if (items.length === 0) {
      setError("请至少添加一条商品行")
      return
    }
    const emptyLine = items.find((it) => !it.product_id || !it.qty || !it.unit_price)
    if (emptyLine) {
      setError("请填写所有商品行的商品编号、数量和售价")
      return
    }

    setSaving(true)
    setError(null)
    try {
      const res = await createSaleBill(
        {
          bill_date: new Date(billDate).toISOString(),
          remark: remark || undefined,
          currency: currency !== "CNY" ? currency : undefined,
          exchange_rate: currency !== "CNY" ? exchangeRate : undefined,
          items: buildLineItems(),
        },
        tenantId
      )
      await draft.markSubmitted()
      router.push(`/sales/${res.bill_id}`)
    } catch (err) {
      setError(String(err))
      setSaving(false)
    }
  }

  return (
    <PageContainer width="wide">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            {isQuick ? "快速收银" : "新建销售单"}
            <DraftBadge status={draft.status} />
          </span>
        }
        subtitle={isQuick ? "一键完成销售出库 + 收款" : "填写销售单信息，保存后生成草稿"}
        actions={
          <Button variant="ghost" size="sm" onClick={() => setIsQuick(!isQuick)}>
            {isQuick ? "切换为草稿模式" : "切换为快速收银"}
          </Button>
        }
      />

      <DraftRestoreToast restoredAt={draft.restoredAt} onDiscard={draft.discardDraft} />

      <form onSubmit={isQuick ? handleQuickCheckout : handleCreateDraft} className="space-y-6">
        {/* Header fields */}
        <div className="space-y-4 rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium text-muted-foreground">基本信息</h2>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="sale-customer">客户名称</Label>
              <Input
                id="sale-customer"
                type="text"
                value={customerName}
                placeholder="可选"
                onChange={(e) => setCustomerName(e.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="sale-date">单据日期</Label>
              <Input
                id="sale-date"
                type="date"
                value={billDate}
                onChange={(e) => setBillDate(e.target.value)}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="sale-remark">备注</Label>
              <Input
                id="sale-remark"
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
          <SaleLineEditor items={items} onChange={setItems} />
        </div>

        {/* Payment section — quick checkout only */}
        {isQuick && (
          <div className="space-y-4 rounded-xl border border-border bg-card p-4">
            <h2 className="text-sm font-medium text-muted-foreground">收款信息</h2>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="sale-paymethod">支付方式</Label>
                <select
                  id="sale-paymethod"
                  className={CONTROL_CLASS}
                  value={paymentMethod}
                  onChange={(e) => setPaymentMethod(e.target.value)}
                >
                  {PAY_METHODS.map((m) => (
                    <option key={m.value} value={m.value}>
                      {m.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="sale-paid">
                  实收金额
                  {totalAmount > 0 && (
                    <span className="ml-2 text-xs font-normal text-muted-foreground">
                      （应收 {formatCNY(totalAmount)}）
                    </span>
                  )}
                </Label>
                <Input
                  id="sale-paid"
                  type="number"
                  value={paidAmount}
                  placeholder={totalAmount > 0 ? totalAmount.toFixed(2) : "0.00"}
                  min="0"
                  step="0.01"
                  onChange={(e) => setPaidAmount(e.target.value)}
                />
              </div>
            </div>

            {/* Change due */}
            {parseFloat(paidAmount) > 0 && parseFloat(paidAmount) > totalAmount && (
              <div className="rounded-md border border-success/30 bg-success/10 px-4 py-2 text-sm text-success">
                找零：{formatCNY(parseFloat(paidAmount) - totalAmount)}
              </div>
            )}
          </div>
        )}

        {error && <ErrorBanner>{error}</ErrorBanner>}

        <div className="flex justify-end gap-3">
          <Button type="button" variant="outline" onClick={() => router.back()} disabled={saving}>
            取消
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? "处理中..." : isQuick ? "确认收银" : "保存草稿"}
          </Button>
        </div>
      </form>
    </PageContainer>
  )
}

export default function NewSalePage() {
  return (
    <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">加载中...</div>}>
      <NewSaleInner />
    </Suspense>
  )
}
