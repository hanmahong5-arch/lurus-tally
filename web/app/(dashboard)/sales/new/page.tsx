"use client"

import { useState, Suspense } from "react"
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

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const PAY_METHODS = [
  { value: "cash", label: "现金" },
  { value: "wechat", label: "微信" },
  { value: "alipay", label: "支付宝" },
  { value: "card", label: "银行卡" },
  { value: "transfer", label: "转账" },
]

function NewSaleInner() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const { profileType } = useProfile()

  // retail profile defaults to quick checkout; explicit ?mode=quick also triggers it
  const defaultQuick = profileType === "retail" || searchParams.get("mode") === "quick"
  const [isQuick, setIsQuick] = useState(defaultQuick)

  const [items, setItems] = useState<SaleLineItem[]>([])
  const [billDate, setBillDate] = useState(
    () => new Date().toISOString().slice(0, 10)
  )
  const [customerName, setCustomerName] = useState("")
  const [remark, setRemark] = useState("")
  const [paymentMethod, setPaymentMethod] = useState("cash")
  const [paidAmount, setPaidAmount] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Multi-currency fields (cross_border profile, Story 9.1)
  const [currency, setCurrency] = useState("CNY")
  const [exchangeRate, setExchangeRate] = useState("1")

  const inputCls =
    "w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"

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
        devTenantId
      )
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
        devTenantId
      )
      router.push(`/sales/${res.bill_id}`)
    } catch (err) {
      setError(String(err))
      setSaving(false)
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">
            {isQuick ? "快速收银" : "新建销售单"}
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {isQuick
              ? "一键完成销售出库 + 收款"
              : "填写销售单信息，保存后生成草稿"}
          </p>
        </div>
        <button
          type="button"
          onClick={() => setIsQuick(!isQuick)}
          className="text-xs text-muted-foreground hover:text-foreground underline"
        >
          {isQuick ? "切换为草稿模式" : "切换为快速收银"}
        </button>
      </div>

      <form
        onSubmit={isQuick ? handleQuickCheckout : handleCreateDraft}
        className="space-y-6"
      >
        {/* Header fields */}
        <div className="rounded-xl border border-border bg-card p-4 space-y-4">
          <h2 className="text-sm font-medium text-muted-foreground">基本信息</h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="space-y-1">
              <label className="text-sm font-medium">客户名称</label>
              <input
                type="text"
                className={inputCls}
                value={customerName}
                placeholder="可选"
                onChange={(e) => setCustomerName(e.target.value)}
              />
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium">单据日期</label>
              <input
                type="date"
                className={inputCls}
                value={billDate}
                onChange={(e) => setBillDate(e.target.value)}
              />
            </div>
            <div className="space-y-1 sm:col-span-2">
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
          <SaleLineEditor items={items} onChange={setItems} />
        </div>

        {/* Payment section — quick checkout only */}
        {isQuick && (
          <div className="rounded-xl border border-border bg-card p-4 space-y-4">
            <h2 className="text-sm font-medium text-muted-foreground">收款信息</h2>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-1">
                <label className="text-sm font-medium">支付方式</label>
                <select
                  className={inputCls}
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
              <div className="space-y-1">
                <label className="text-sm font-medium">
                  实收金额
                  {totalAmount > 0 && (
                    <span className="ml-2 text-xs text-muted-foreground font-normal">
                      （应收 ¥ {totalAmount.toFixed(2)}）
                    </span>
                  )}
                </label>
                <input
                  type="number"
                  className={inputCls}
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
              <div className="rounded-md bg-green-500/10 border border-green-500/30 px-4 py-2 text-sm text-green-700">
                找零：¥ {(parseFloat(paidAmount) - totalAmount).toFixed(2)}
              </div>
            )}
          </div>
        )}

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
            {saving
              ? "处理中..."
              : isQuick
              ? "确认收银"
              : "保存草稿"}
          </button>
        </div>
      </form>
    </div>
  )
}

export default function NewSalePage() {
  return (
    <Suspense fallback={<div className="p-6 text-muted-foreground">加载中...</div>}>
      <NewSaleInner />
    </Suspense>
  )
}
