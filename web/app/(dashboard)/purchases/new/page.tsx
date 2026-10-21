"use client"

import { useState, useEffect, useRef, Suspense } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import {
  createPurchaseBill,
  getPurchaseBill,
  updatePurchaseBill,
  type BillLineItemInput,
} from "@/lib/api/purchase"
import { getProduct } from "@/lib/api/products"
import { listProductUnits } from "@/lib/api/product-units"
import { getSupplier } from "@/lib/api/suppliers"
import { BillLineEditor, type BillLineItem } from "@/components/bill-line-editor"
import { PurchaseDraftLines } from "@/components/purchase-draft-lines"
import { ProfileGate } from "@/lib/profile"
import { CurrencySelector } from "@/components/cross-border/currency-selector"
import { RateInput } from "@/components/cross-border/rate-input"
import { useDraft } from "@/hooks/useDraft"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { toast } from "sonner"
import { DraftBadge } from "@/components/draft/DraftBadge"
import { DraftRestoreToast } from "@/components/draft/DraftRestoreToast"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { EmptyState } from "@/components/ui/empty-state"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { SupplierPicker } from "@/components/entity-pickers"
import { WarehouseSelect } from "@/components/warehouse-select"
import { useSubmitKey } from "@/lib/submit-key"
import { extractApiError } from "@/lib/api/errors"
import { takeBillCopy } from "@/lib/bill-copy"
import { todayCST } from "@/lib/date"
import { lineCents } from "@/lib/cents"

const CONTROL_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"

/** Fields persisted as a purchase bill draft. */
interface PurchaseDraft {
  supplierId: string
  supplierName: string
  warehouseId: string
  items: BillLineItem[]
  billDate: string
  shippingFee: string
  taxAmount: string
  remark: string
  currency: string
  exchangeRate: string
}

/** Built per mount so a tab left open overnight does not default to yesterday. */
const purchaseInitial = (): PurchaseDraft => ({
  supplierId: "",
  supplierName: "",
  warehouseId: "",
  items: [],
  billDate: todayCST(),
  shippingFee: "0",
  taxAmount: "0",
  remark: "",
  currency: "CNY",
  exchangeRate: "1",
})

function NewPurchaseInner() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const tenantId = useTenantId()
  const { known: roleKnown, can } = useRole()
  // Someone without cost.view drafts products and quantities only; prices, freight, tax and
  // currency are the boss's or manager's to fill in before approving.
  const priceBlind = roleKnown && !can("cost.view")
  const submitKey = useSubmitKey()
  // ?edit=<id>: change a draft in place instead of creating a new bill.
  const editId = searchParams.get("edit")

  const draft = useDraft<PurchaseDraft>(editId ? `draft:purchase:edit:${editId}` : "draft:purchase:new", purchaseInitial())

  const [supplierId, setSupplierId] = useState(draft.value.supplierId ?? "")
  const [supplierName, setSupplierName] = useState(draft.value.supplierName ?? "")
  const [warehouseId, setWarehouseId] = useState(draft.value.warehouseId ?? "")
  const [items, setItems] = useState<BillLineItem[]>(draft.value.items ?? [])
  const [billDate, setBillDate] = useState(draft.value.billDate ?? todayCST())
  const [shippingFee, setShippingFee] = useState(draft.value.shippingFee ?? "0")
  const [taxAmount, setTaxAmount] = useState(draft.value.taxAmount ?? "0")
  const [remark, setRemark] = useState(draft.value.remark ?? "")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
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
    const copy = takeBillCopy("purchase")
    if (!copy) return
    copiedRef.current = true
    setSupplierId(copy.partner_id ?? "")
    setSupplierName(copy.partner_name ?? "")
    setWarehouseId(copy.warehouse_id ?? "")
    setRemark(copy.remark ?? "")
    setCurrency(copy.currency ?? "CNY")
    setExchangeRate(copy.exchange_rate ?? "1")
    setShippingFee(copy.shipping_fee ?? "0")
    setTaxAmount(copy.tax_amount ?? "0")
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

  // ?prefill_product_id=<id> ("去补货" on the to-do list): start with one line for that
  // product. Like a parked copy it wins over a restored draft; the line editor fills the
  // unit and the last price.
  const prefillId = editId ? null : searchParams.get("prefill_product_id")
  if (prefillId && !copiedRef.current) copiedRef.current = true
  useEffect(() => {
    if (!prefillId) return
    let cancelled = false
    getProduct(prefillId, tenantId)
      .then(async (p) => {
        // The SKU price is quoted in the product's default unit, but the line starts on the
        // base unit: only carry the price over when they are the same unit.
        let price = "0"
        if (p.purchase_price) {
          if (!p.default_unit_id) {
            price = p.purchase_price
          } else {
            const units = await listProductUnits(p.id, { tenantId }).catch(() => [])
            if (units.find((o) => o.is_base)?.id === p.default_unit_id) price = p.purchase_price
          }
        }
        if (cancelled) return
        setItems((prev) =>
          prev.some((it) => it.product_id === p.id)
            ? prev
            : [
                ...prev,
                {
                  product_id: p.id,
                  product_name: p.name,
                  line_no: prev.length + 1,
                  qty: "1",
                  unit_price: price,
                  _subtotal: price,
                },
              ]
        )
      })
      .catch(() => {
        if (!cancelled) setError("没能带出要补货的商品，请在下面手动添加")
      })
    return () => {
      cancelled = true
    }
  }, [prefillId, tenantId])

  // Edit mode: read the draft back and prefill the form with it.
  useEffect(() => {
    if (!editId) return
    let cancelled = false
    getPurchaseBill(editId, tenantId)
      .then(async (d) => {
        if (cancelled) return
        if (d.head.status !== 0) {
          setError("只有草稿采购单可以修改，这张单已经审核或作废了")
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
            ? getSupplier(d.head.partner_id, tenantId).then(
                (s) => {
                  names[d.head.partner_id as string] = s.name
                },
                () => {}
              )
            : Promise.resolve(),
        ])
        if (cancelled) return
        const day = d.head.bill_date.slice(0, 10)
        const money = d.head as { currency?: string; exchange_rate?: string }
        setSupplierId(d.head.partner_id ?? "")
        setSupplierName(d.head.partner_name || names[d.head.partner_id ?? ""] || "")
        setWarehouseId(d.head.warehouse_id ?? "")
        setRemark(d.head.remark ?? "")
        setBillDate(day)
        setOrigBillDate(day)
        setShippingFee(d.head.shipping_fee ?? "0")
        setTaxAmount(d.head.tax_amount ?? "0")
        // Currency and rate are fixed when the bill is created; shown for reference only.
        setCurrency(money.currency || "CNY")
        setExchangeRate(money.exchange_rate || "1")
        setItems(
          d.items.map((it, i) => ({
            product_id: it.product_id,
            product_name: names[it.product_id],
            unit_id: it.unit_id,
            unit_name: it.unit_name,
            line_no: i + 1,
            qty: it.qty,
            unit_price: it.unit_price ?? "0",
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
    setSupplierId(draft.value.supplierId ?? "")
    setSupplierName(draft.value.supplierName ?? "")
    setWarehouseId(draft.value.warehouseId ?? "")
    setItems(draft.value.items ?? [])
    setBillDate(draft.value.billDate ?? todayCST())
    setShippingFee(draft.value.shippingFee ?? "0")
    setTaxAmount(draft.value.taxAmount ?? "0")
    setRemark(draft.value.remark ?? "")
    setCurrency(draft.value.currency ?? "CNY")
    setExchangeRate(draft.value.exchangeRate ?? "1")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft.restoredAt])

  // Persist field changes to draft (debounced inside useDraft).
  useEffect(() => {
    draft.setValue({ supplierId, supplierName, warehouseId, items, billDate, shippingFee, taxAmount, remark, currency, exchangeRate })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [supplierId, supplierName, warehouseId, items, billDate, shippingFee, taxAmount, remark, currency, exchangeRate])

  // Clerks may draft purchases but cannot open them (the detail shows cost), so send them back to the list.
  function goAfterSave(id: string) {
    if (priceBlind) {
      toast.success("采购草稿已保存，等老板或店长审核")
      router.push("/purchases")
      return
    }
    router.push(`/purchases/${id}`)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!supplierId) {
      setError("请选择供应商")
      return
    }
    if (items.length === 0) {
      setError("请至少添加一条商品行")
      return
    }
    const emptyLine = items.find((it) => !it.product_id || !it.qty || (!priceBlind && !it.unit_price))
    if (emptyLine) {
      setError(priceBlind ? "请填写所有商品行的商品和数量" : "请填写所有商品行的商品、数量和单价")
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
        unit_price: priceBlind ? "0" : it.unit_price,
      }))

      if (editId) {
        // The date is only sent when the clerk changed it, so the draft keeps its exact time otherwise.
        await updatePurchaseBill(
          editId,
          {
            partner_id: supplierId,
            warehouse_id: warehouseId || undefined,
            bill_date: billDate !== origBillDate ? new Date(billDate).toISOString() : undefined,
            shipping_fee: shippingFee,
            tax_amount: taxAmount,
            remark: remark || undefined,
            items: lineItems,
          },
          tenantId
        )
        await draft.markSubmitted()
        goAfterSave(editId)
        return
      }

      const body = {
          partner_id: supplierId,
          warehouse_id: warehouseId || undefined,
          bill_date: new Date(billDate).toISOString(),
          shipping_fee: shippingFee,
          tax_amount: taxAmount,
          remark: remark || undefined,
          currency: currency !== "CNY" ? currency : undefined,
          exchange_rate: currency !== "CNY" ? exchangeRate : undefined,
          items: lineItems,
      }
      const res = await createPurchaseBill(body, tenantId, submitKey.keyFor(body))
      submitKey.succeeded()
      await draft.markSubmitted()
      goAfterSave(res.bill_id)
    } catch (e) {
      submitKey.failed(e)
      setError(extractApiError(e).message)
      setSaving(false)
    }
  }

  return (
    <PageContainer width="wide">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            {editId ? "修改采购单" : "新建采购单"}
            <DraftBadge status={draft.status} />
          </span>
        }
        subtitle={editId ? "改好商品和数量后保存，单号不变" : "填写采购单信息，保存后将生成草稿"}
      />

      {!editId && !copiedRef.current && <DraftRestoreToast restoredAt={draft.restoredAt} onDiscard={draft.discardDraft} />}

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Header fields */}
        <div className="space-y-4 rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium text-muted-foreground">基本信息</h2>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="bill-supplier">供应商</Label>
              <SupplierPicker
                id="bill-supplier"
                value={supplierId}
                label={supplierName}
                placeholder="搜索或新建供应商"
                disabled={saving}
                onSelect={(s) => {
                  setSupplierId(s.id)
                  setSupplierName(s.name)
                }}
                onClear={() => {
                  setSupplierId("")
                  setSupplierName("")
                }}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="bill-warehouse">入库仓库</Label>
              <WarehouseSelect
                id="bill-warehouse"
                value={warehouseId}
                onChange={setWarehouseId}
                tenantId={tenantId}
                disabled={saving}
              />
            </div>
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
          {!priceBlind && (
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
                    disabled={saving || !!editId}
                  />
                  {editId && <p className="text-xs text-muted-foreground">币种开单后不能改</p>}
                </div>
                {currency !== "CNY" && (
                  <div className="space-y-1.5">
                    <Label>汇率（→ CNY）</Label>
                    <RateInput
                      currency={currency}
                      value={exchangeRate}
                      onChange={setExchangeRate}
                      date={billDate}
                      disabled={saving || !!editId}
                      className={CONTROL_CLASS}
                    />
                  </div>
                )}
              </div>
            </ProfileGate>
          )}
        </div>

        {/* Line items */}
        <div className="rounded-xl border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">商品明细</h2>
          {priceBlind ? (
            <PurchaseDraftLines items={items} onChange={setItems} />
          ) : (
            <BillLineEditor
              items={items}
              onChange={setItems}
              partnerId={supplierId || undefined}
              shippingFee={shippingFee}
              taxAmount={taxAmount}
              onShippingFeeChange={setShippingFee}
              onTaxAmountChange={setTaxAmount}
            />
          )}
        </div>

        {error && <ErrorBanner>{error}</ErrorBanner>}

        <div className="flex justify-end gap-3">
          <Button type="button" variant="outline" onClick={() => router.back()} disabled={saving}>
            取消
          </Button>
          <Button type="submit" disabled={saving || (!!editId && !editReady)}>
            {saving ? "保存中..." : editId ? "保存修改" : "保存草稿"}
          </Button>
        </div>
      </form>
    </PageContainer>
  )
}

export default function NewPurchasePage() {
  const { known, can } = useRole()
  if (known && !can("bill.draft")) {
    return (
      <PageContainer width="narrow">
        <PageHeader title="新建采购单" />
        <EmptyState title="你没有开单的权限" description="开单请找老板或店长" />
      </PageContainer>
    )
  }
  return (
    <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">加载中...</div>}>
      <NewPurchaseInner />
    </Suspense>
  )
}
