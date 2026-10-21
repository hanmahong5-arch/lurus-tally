"use client"

import { useState, useEffect, useRef, Suspense } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import Link from "next/link"
import { toast } from "sonner"
import {
  createSaleBill,
  getSaleBill,
  quickCheckout,
  updateSaleBill,
  type BillLineItemInput,
} from "@/lib/api/sale"
import { getProduct } from "@/lib/api/products"
import { getCustomer } from "@/lib/api/customers"
import { SaleLineEditor, type SaleLineItem } from "@/components/sale-line-editor"
import { ProfileGate, useProfile } from "@/lib/profile"
import { CurrencySelector } from "@/components/cross-border/currency-selector"
import { RateInput } from "@/components/cross-border/rate-input"
import { useDraft } from "@/hooks/useDraft"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { DraftBadge } from "@/components/draft/DraftBadge"
import { DraftRestoreToast } from "@/components/draft/DraftRestoreToast"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { EmptyState } from "@/components/ui/empty-state"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { formatCNY } from "@/lib/format"
import { lineCents, roundCents } from "@/lib/cents"
import { CustomerPicker } from "@/components/entity-pickers"
import { WarehouseSelect } from "@/components/warehouse-select"
import { useSubmitKey } from "@/lib/submit-key"
import { extractApiError } from "@/lib/api/errors"
import { describeStockShortage } from "@/lib/stock-shortage"
import { takeBillCopy } from "@/lib/bill-copy"
import { todayCST } from "@/lib/date"

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
  customerId: string
  customerName: string
  warehouseId: string
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
  // Quick checkout is for people who may check out; drafting is for people who may draft.
  // An unknown role hides nothing (the backend decides).
  const { known: roleKnown, can } = useRole()
  const mayCheckout = !roleKnown || can("bill.checkout")
  const mayDraft = !roleKnown || can("bill.draft")
  const createKey = useSubmitKey()
  const checkoutKey = useSubmitKey()
  // ?edit=<id>: change a draft in place instead of creating a new bill.
  const editId = searchParams.get("edit")

  // retail profile defaults to quick checkout; explicit ?mode=quick also triggers it
  const defaultQuick = profileType === "retail" || searchParams.get("mode") === "quick"

  const SALE_INITIAL: SaleDraft = {
    isQuick: defaultQuick,
    items: [],
    billDate: todayCST(),
    customerId: "",
    customerName: "",
    warehouseId: "",
    remark: "",
    paymentMethod: "cash",
    paidAmount: "",
    currency: "CNY",
    exchangeRate: "1",
  }

  const draft = useDraft<SaleDraft>(editId ? `draft:sale:edit:${editId}` : "draft:sale:new", SALE_INITIAL)

  const [isQuickPref, setIsQuick] = useState(editId ? false : (draft.value.isQuick ?? defaultQuick))
  const isQuick = !editId && mayCheckout && (isQuickPref || !mayDraft)
  const [items, setItems] = useState<SaleLineItem[]>(draft.value.items ?? [])
  const [billDate, setBillDate] = useState(draft.value.billDate ?? todayCST())
  const [customerId, setCustomerId] = useState(draft.value.customerId ?? "")
  const [customerName, setCustomerName] = useState(draft.value.customerName ?? "")
  const [warehouseId, setWarehouseId] = useState(draft.value.warehouseId ?? "")
  const [remark, setRemark] = useState(draft.value.remark ?? "")
  const [paymentMethod, setPaymentMethod] = useState(draft.value.paymentMethod ?? "cash")
  const [paidAmount, setPaidAmount] = useState(draft.value.paidAmount ?? "")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Set when the checkout failed on stock, so the banner offers the way out.
  const [shortStock, setShortStock] = useState(false)
  // Edit mode: the form only becomes submittable once the draft has been read back.
  const [editReady, setEditReady] = useState(false)
  const [origBillDate, setOrigBillDate] = useState("")

  // Multi-currency fields (cross_border profile, Story 9.1)
  const [currency, setCurrency] = useState(draft.value.currency ?? "CNY")
  const [exchangeRate, setExchangeRate] = useState(draft.value.exchangeRate ?? "1")

  // "复制为新单": take the parked copy once on mount; it wins over a restored draft.
  const copiedRef = useRef(!!editId)
  useEffect(() => {
    if (editId) return
    const copy = takeBillCopy("sale")
    if (!copy) return
    copiedRef.current = true
    setCustomerId(copy.partner_id ?? "")
    setCustomerName(copy.partner_name ?? "")
    setWarehouseId(copy.warehouse_id ?? "")
    setRemark(copy.remark ?? "")
    setCurrency(copy.currency ?? "CNY")
    setExchangeRate(copy.exchange_rate ?? "1")
    setItems(
      copy.items.map((it, i) => ({
        product_id: it.product_id,
        product_name: it.product_name,
        unit_id: it.unit_id,
        unit_name: it.unit_name,
        line_no: i + 1,
        qty: it.qty,
        unit_price: it.unit_price,
        _subtotal: lineCents(parseFloat(it.qty) || 0, parseFloat(it.unit_price) || 0).toFixed(2),
        // Keep the original bill's prices: copying must not silently re-price.
        _price_touched: true,
      }))
    )
    // Mount-only: the parked copy is taken once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Edit mode: read the draft back and prefill the form with it.
  useEffect(() => {
    if (!editId) return
    let cancelled = false
    getSaleBill(editId, tenantId)
      .then(async (d) => {
        if (cancelled) return
        if (d.head.status !== 0) {
          setError("只有草稿销售单可以修改，这张单已经审核或作废了")
          return
        }
        const names: Record<string, string> = {}
        await Promise.all([
          ...Array.from(new Set(d.items.map((it) => it.product_id))).map((pid) =>
            getProduct(pid, tenantId).then(
              (p) => {
                names[pid] = p.name
              },
              () => {}
            )
          ),
          d.head.partner_id
            ? getCustomer(d.head.partner_id, tenantId).then(
                (c) => {
                  names[d.head.partner_id as string] = c.name
                },
                () => {}
              )
            : Promise.resolve(),
        ])
        if (cancelled) return
        const day = d.head.bill_date.slice(0, 10)
        setCustomerId(d.head.partner_id ?? "")
        setCustomerName(d.head.partner_name || names[d.head.partner_id ?? ""] || "")
        setWarehouseId(d.head.warehouse_id ?? "")
        setRemark(d.head.remark ?? "")
        setBillDate(day)
        setOrigBillDate(day)
        setItems(
          d.items.map((it, i) => ({
            product_id: it.product_id,
            product_name: names[it.product_id],
            unit_id: it.unit_id,
            unit_name: it.unit_name,
            line_no: i + 1,
            qty: it.qty,
            unit_price: it.unit_price,
            _subtotal: it.line_amount,
            // The draft keeps its own prices: editing must not silently re-price.
            _price_touched: true,
          }))
        )
        setEditReady(true)
      })
      .catch((e) => {
        if (!cancelled) setError(extractApiError(e).message)
      })
    return () => {
      cancelled = true
    }
  }, [editId, tenantId])

  // When draft is restored from IDB (restoredAt flips non-null), sync local state.
  useEffect(() => {
    if (!draft.restoredAt || copiedRef.current) return
    setIsQuick(draft.value.isQuick ?? defaultQuick)
    setItems(draft.value.items ?? [])
    setBillDate(draft.value.billDate ?? todayCST())
    setCustomerId(draft.value.customerId ?? "")
    setCustomerName(draft.value.customerName ?? "")
    setWarehouseId(draft.value.warehouseId ?? "")
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
      customerId,
      customerName,
      warehouseId,
      remark,
      paymentMethod,
      paidAmount,
      currency,
      exchangeRate,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isQuick, items, billDate, customerId, customerName, warehouseId, remark, paymentMethod, paidAmount, currency, exchangeRate])

  // Each line is kept to the cent, then summed: the same figure the server books.
  const totalAmount = roundCents(
    items.reduce((acc, it) => acc + lineCents(parseFloat(it.qty) || 0, parseFloat(it.unit_price) || 0), 0)
  )

  function buildLineItems(withWarehouse = false): BillLineItemInput[] {
    return items.map((it, idx) => ({
      product_id: it.product_id,
      unit_id: it.unit_id,
      unit_name: it.unit_name,
      line_no: idx + 1,
      qty: it.qty,
      unit_price: it.unit_price,
      warehouse_id: withWarehouse ? warehouseId || undefined : undefined,
    }))
  }

  async function handleQuickCheckout(e: React.FormEvent) {
    e.preventDefault()
    if (items.length === 0) {
      setShortStock(false)
      setError("请至少添加一条商品行")
      return
    }
    const emptyLine = items.find((it) => !it.product_id || !it.qty || !it.unit_price)
    if (emptyLine) {
      setShortStock(false)
      setError("请填写所有商品行的商品、数量和售价")
      return
    }
    const paid = paidAmount.trim()
    if (!/^\d+(\.\d+)?$/.test(paid) || parseFloat(paid) <= 0) {
      setShortStock(false)
      setError("请输入实收金额")
      return
    }

    setSaving(true)
    setError(null)
    setShortStock(false)
    try {
      const body = {
        partner_id: customerId || undefined,
        customer_name: customerId ? undefined : customerName || undefined,
        items: buildLineItems(true),
        payment_method: paymentMethod,
        paid_amount: paid,
        remark: remark || undefined,
      }
      const res = await quickCheckout(body, tenantId, checkoutKey.keyFor(body))
      checkoutKey.succeeded()
      await draft.markSubmitted()
      const change = parseFloat(res.change_amount ?? "0")
      if (change > 0) toast.info(`找零 ${formatCNY(change)}（不计入收款）`)
      router.push(`/sales/${res.bill_id}`)
    } catch (err) {
      checkoutKey.failed(err)
      const names: Record<string, string> = {}
      for (const it of items) if (it.product_id && it.product_name) names[it.product_id] = it.product_name
      const shortage = await describeStockShortage(err, tenantId, names)
      setShortStock(shortage !== null)
      setError(shortage ?? extractApiError(err).message)
      setSaving(false)
    }
  }

  async function handleCreateDraft(e: React.FormEvent) {
    e.preventDefault()
    if (items.length === 0) {
      setShortStock(false)
      setError("请至少添加一条商品行")
      return
    }
    const emptyLine = items.find((it) => !it.product_id || !it.qty || !it.unit_price)
    if (emptyLine) {
      setShortStock(false)
      setError("请填写所有商品行的商品、数量和售价")
      return
    }

    setSaving(true)
    setError(null)
    setShortStock(false)
    try {
      if (editId) {
        // The date is only sent when the clerk changed it, so the draft keeps its exact time otherwise.
        await updateSaleBill(
          editId,
          {
            partner_id: customerId || undefined,
            warehouse_id: warehouseId || undefined,
            bill_date: billDate !== origBillDate ? new Date(billDate).toISOString() : undefined,
            remark: remark || undefined,
            items: buildLineItems(),
          },
          tenantId
        )
        await draft.markSubmitted()
        router.push(`/sales/${editId}`)
        return
      }
      const body = {
          partner_id: customerId || undefined,
          warehouse_id: warehouseId || undefined,
          bill_date: new Date(billDate).toISOString(),
          remark: remark || undefined,
          currency: currency !== "CNY" ? currency : undefined,
          exchange_rate: currency !== "CNY" ? exchangeRate : undefined,
          items: buildLineItems(),
      }
      const res = await createSaleBill(body, tenantId, createKey.keyFor(body))
      createKey.succeeded()
      await draft.markSubmitted()
      router.push(`/sales/${res.bill_id}`)
    } catch (err) {
      createKey.failed(err)
      setShortStock(false)
      setError(extractApiError(err).message)
      setSaving(false)
    }
  }

  return (
    <PageContainer width="wide">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            {editId ? "修改销售单" : isQuick ? "快速收银" : "新建销售单"}
            <DraftBadge status={draft.status} />
          </span>
        }
        subtitle={
          editId ? "改好商品和数量后保存，单号不变" : isQuick ? "一键完成销售出库 + 收款" : "填写销售单信息，保存后生成草稿"
        }
        actions={
          editId || !mayCheckout || !mayDraft ? undefined : (
            <Button variant="ghost" size="sm" onClick={() => setIsQuick(!isQuick)}>
              {isQuick ? "切换为草稿模式" : "切换为快速收银"}
            </Button>
          )
        }
      />

      {!editId && <DraftRestoreToast restoredAt={draft.restoredAt} onDiscard={draft.discardDraft} />}

      <form onSubmit={isQuick ? handleQuickCheckout : handleCreateDraft} className="space-y-6">
        {/* Header fields */}
        <div className="space-y-4 rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium text-muted-foreground">基本信息</h2>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="sale-customer">客户</Label>
              <CustomerPicker
                id="sale-customer"
                value={customerId}
                label={customerName}
                placeholder="可选，搜索或新建客户"
                disabled={saving}
                onSelect={(c) => {
                  setCustomerId(c.id)
                  setCustomerName(c.name)
                }}
                onClear={() => {
                  setCustomerId("")
                  setCustomerName("")
                }}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="sale-warehouse">出库仓库</Label>
              <WarehouseSelect
                id="sale-warehouse"
                value={warehouseId}
                onChange={setWarehouseId}
                tenantId={tenantId}
                disabled={saving}
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
                placeholder={profileType === "horticulture" ? "送到哪个工地、联系人和电话写在这里，会印在送货单上" : "可选"}
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
          <SaleLineEditor items={items} onChange={setItems} partnerId={customerId || undefined} />
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
                <div className="flex gap-2">
                  <Input
                    id="sale-paid"
                    type="number"
                    value={paidAmount}
                    placeholder={totalAmount > 0 ? `应收 ${formatCNY(totalAmount)}` : "0.00"}
                    min="0"
                    step="0.01"
                    onChange={(e) => setPaidAmount(e.target.value)}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={totalAmount <= 0}
                    onClick={() => setPaidAmount(totalAmount.toFixed(2))}
                  >
                    全额
                  </Button>
                </div>
              </div>
            </div>

            {/* Change due */}
            {parseFloat(paidAmount) > 0 && parseFloat(paidAmount) > totalAmount && (
              <div className="rounded-md border border-success/30 bg-success/10 px-4 py-2 text-sm text-success">
                找零：{formatCNY(roundCents(parseFloat(paidAmount) - totalAmount))}
              </div>
            )}
          </div>
        )}

        {error && (
          <ErrorBanner
            hint={
              shortStock && (
                <Link href="/stock" className="underline">
                  去盘点或进货
                </Link>
              )
            }
          >
            {error}
          </ErrorBanner>
        )}

        <div className="flex justify-end gap-3">
          <Button type="button" variant="outline" onClick={() => router.back()} disabled={saving}>
            取消
          </Button>
          <Button type="submit" disabled={saving || (!!editId && !editReady)}>
            {saving ? "处理中..." : editId ? "保存修改" : isQuick ? "确认收银" : "保存草稿"}
          </Button>
        </div>
      </form>
    </PageContainer>
  )
}

export default function NewSalePage() {
  const { known, can } = useRole()
  if (known && !can("bill.draft") && !can("bill.checkout")) {
    return (
      <PageContainer width="narrow">
        <PageHeader title="新建销售单" />
        <EmptyState title="你没有开单的权限" description="开单请找老板或店长" />
      </PageContainer>
    )
  }
  return (
    <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">加载中...</div>}>
      <NewSaleInner />
    </Suspense>
  )
}
