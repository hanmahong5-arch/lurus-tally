"use client"

import { useState, useCallback } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import {
  getPurchaseBill,
  approvePurchaseBill,
  cancelPurchaseBill,
  restorePurchaseBill,
  returnPurchaseBill,
  type BillDetail,
  BILL_STATUS_LABEL,
} from "@/lib/api/purchase"
import { BILL_STATUS_TONE } from "@/lib/status"
import { globalUndoStack } from "@/lib/undo/undo-stack"
import { BillLineEditor, type BillLineItem } from "@/components/bill-line-editor"
import { PurchaseDraftLines } from "@/components/purchase-draft-lines"
import { useConfirm } from "@/hooks/useConfirm"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { useProductNames } from "@/hooks/use-product-names"
import { useBillPartyNames } from "@/hooks/use-bill-party-names"
import { BillInvoices } from "@/components/bill-invoices"
import { AuditTrail } from "@/components/audit-trail"
import { PaymentForm } from "@/components/payment-form"
import { PurchaseReturnDialog, returnablePurchaseQty } from "@/components/purchase-return-dialog"
import { useSubmitKey } from "@/lib/submit-key"
import { listPayments, type Payment } from "@/lib/api/payment"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { formatCNY, formatDate, formatDateTime } from "@/lib/format"
import { extractApiError } from "@/lib/api/errors"
import { getProduct } from "@/lib/api/products"
import { saveBillCopy } from "@/lib/bill-copy"
import { cn } from "@/lib/utils"

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

const PAY_METHOD_LABEL: Record<string, string> = {
  transfer: "转账",
  cash: "现金",
  wechat: "微信",
  alipay: "支付宝",
  card: "银行卡",
}

export default function PurchaseDetailPage() {
  const { id } = useParams<{ id: string }>()
  const router = useRouter()

  const [detail, setDetail] = useState<BillDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [acting, setActing] = useState(false)
  const [payments, setPayments] = useState<Payment[]>([])

  // Return-to-supplier dialog
  const [showReturn, setShowReturn] = useState(false)
  const [returning, setReturning] = useState(false)
  const [returnError, setReturnError] = useState<string | null>(null)
  const [returnNotice, setReturnNotice] = useState<string | null>(null)
  const [returnBillId, setReturnBillId] = useState<string | null>(null)
  const returnKey = useSubmitKey()

  // Optional payment made together with the approval
  const [paidAmount, setPaidAmount] = useState("")
  const [payMethod, setPayMethod] = useState("transfer")
  // The payment list failed to load: the figures above still come from the bill itself.
  const [paymentsFailed, setPaymentsFailed] = useState(false)

  const [copying, setCopying] = useState(false)
  const confirm = useConfirm()
  const tenantId = useTenantId()
  // The backend enforces these; the root layout hands over the session, so the role is known on first render
  // and an unknown role (signed out) is shown nothing.
  const { known: roleKnown, can } = useRole()
  const mayApprove = can("bill.approve")
  const mayCancel = can("bill.cancel")
  const mayReturn = can("bill.return")
  const mayEdit = can("bill.draft")
  const mayRecordPayment = can("payment.record")
  // The payment list is only requested once the role is known to allow it: an
  // unknown role would ask first and a restricted one would get a 403.
  const fetchPayments = roleKnown && can("payment.read")
  // Without cost.view the backend sends no price or amount: the page shows products and quantities only.
  const priceBlind = roleKnown && !can("cost.view")
  const productNames = useProductNames(detail?.items.map((it) => it.product_id) ?? [], tenantId)
  const names = useBillPartyNames({
    kind: "supplier",
    partnerId: detail?.head.partner_id,
    partnerName: detail?.head.partner_name,
    warehouseId: detail?.head.warehouse_id,
    warehouseName: detail?.head.warehouse_name,
    tenantId,
  })

  // silent = re-read in place (no skeleton), used after a payment so the open page stays put.
  const load = useCallback((signal?: AbortSignal, isCancelled?: () => boolean, silent = false) => {
    if (!silent) setLoading(true)
    setError(null)
    return getPurchaseBill(id, tenantId, signal)
      .then(async (data) => {
        if (isCancelled?.()) return
        // Payment history only exists once the bill is approved.
        let paid: Payment[] = []
        let failed = false
        if (data.head.status === 2 && fetchPayments) {
          paid = await listPayments(id, tenantId).catch(() => {
            failed = true
            return [] as Payment[]
          })
        }
        if (isCancelled?.()) return
        setDetail(data)
        setPayments(paid)
        setPaymentsFailed(failed)
      })
      .catch((e) => {
        if (isCancelled?.() || signal?.aborted) return
        // A failed in-place refresh must not blank the bill the user is looking at.
        if (silent) setActionError(extractApiError(e).message)
        else setError(extractApiError(e).message)
      })
      .finally(() => {
        if (isCancelled?.()) return
        setLoading(false)
      })
  }, [id, tenantId, fetchPayments])

  // The bill itself needs purchase.view; without it no request is sent.
  const mayView = can("purchase.view")
  useAbortableEffect((signal, isCancelled) => {
    if (!mayView) return
    load(signal, isCancelled)
  }, [load, mayView])

  async function handleApprove() {
    const paid = parseFloat(paidAmount)
    const payNote = paid > 0 ? `审核时将同时登记付款 ${formatCNY(paid)}（${PAY_METHOD_LABEL[payMethod] ?? payMethod}）。` : ""
    // Lines still without a price: the only ways forward are to fix them or to book them as gifts.
    const allowZeroPrice = zeroPriceItems.length > 0
    if (allowZeroPrice) {
      const gift = await confirm({
        title: `还有 ${zeroPriceItems.length} 行没填进货价`,
        body: `${zeroPriceNames}的进货价是 0。请先修改补上价格；如果确实是赠品，可按 0 入账，这些商品的成本会记为 0。${payNote}`,
        confirmText: "确认是赠品，按 0 入账",
        cancelText: "去修改",
        distinguishDismiss: true,
      })
      if (!gift) {
        // Only the "去修改" button leads to the edit page; Esc / closing just stays here.
        if (gift === false && mayEdit) router.push(`/purchases/new?edit=${id}`)
        return
      }
    } else {
      const ok = await confirm({
        title: "审核通过此采购单",
        body: `审核后不可取消，将自动更新库存。${payNote}`,
        confirmText: "确认审核",
      })
      if (!ok) return
    }
    setActing(true)
    setActionError(null)
    try {
      await approvePurchaseBill(
        id,
        tenantId,
        paid > 0 || allowZeroPrice
          ? {
              ...(paid > 0 ? { paid_amount: paidAmount.trim(), payment_method: payMethod } : {}),
              ...(allowZeroPrice ? { allow_zero_price: true } : {}),
            }
          : undefined
      )
      setPaidAmount("")
      load()
    } catch (e) {
      setActionError(extractApiError(e).message)
    } finally {
      setActing(false)
    }
  }

  async function handleReturn(lines: { bill_item_id: string; qty: string }[], remark: string) {
    const body = { items: lines, remark: remark || undefined }
    setReturning(true)
    setReturnError(null)
    try {
      const res = await returnPurchaseBill(id, body, tenantId, returnKey.keyFor(body))
      returnKey.succeeded()
      setShowReturn(false)
      setReturnBillId(res.bill_id)
      const owes = parseFloat(res.supplier_owes) || 0
      setReturnNotice(
        `退货成功，退货单号 ${res.bill_no}，退货金额 ${formatCNY(parseFloat(res.return_amount))}，` +
          `欠供应商的钱减少 ${formatCNY(parseFloat(res.payable_reduced))}` +
          (owes > 0 ? `；这张单已经付过款，供应商还应退给你 ${formatCNY(owes)}，请和供应商线下结算` : "")
      )
      load()
    } catch (e) {
      returnKey.failed(e)
      setReturnError(extractApiError(e).message)
    } finally {
      setReturning(false)
    }
  }

  async function handleCancel() {
    if (!detail) return

    const billNo = detail.head.bill_no

    // Push undo entry BEFORE the cancel call — consistent with product delete pattern.
    globalUndoStack.push({
      type: "cancel_purchase",
      id,
      billNo,
      revert: async () => {
        await restorePurchaseBill(id, tenantId)
        load()
      },
    })

    setActing(true)
    setActionError(null)
    try {
      await cancelPurchaseBill(id, tenantId)
      load()
    } catch (e) {
      globalUndoStack.pop()
      setActionError(extractApiError(e).message)
    } finally {
      setActing(false)
    }
  }

  if (!mayView) {
    return (
      <PageContainer width="default">
        <PageHeader title="采购单" />
        <EmptyState title="你没有查看采购单的权限" description="采购单只有老板、店长、店员和财务可以查看，请找老板或店长" />
      </PageContainer>
    )
  }

  if (loading) {
    return (
      <PageContainer width="wide">
        <Skeleton className="mb-2 h-7 w-48" />
        <Skeleton className="mb-6 h-4 w-64" />
        <Skeleton className="h-40" />
      </PageContainer>
    )
  }

  if (error || !detail) {
    return (
      <PageContainer width="wide">
        <div className="space-y-4">
          <ErrorBanner hint="请刷新页面重试">{error ?? "采购单不存在"}</ErrorBanner>
          <Link href="/purchases" className="text-sm text-primary hover:underline">
            返回列表
          </Link>
        </div>
      </PageContainer>
    )
  }

  const { head, items } = detail
  const lineItems: BillLineItem[] = items.map((it) => ({
    product_id: it.product_id,
    product_name: productNames[it.product_id],
    unit_id: it.unit_id,
    unit_name: it.unit_name,
    line_no: it.line_no,
    qty: it.qty,
    unit_price: it.unit_price ?? "0",
    _subtotal: it.line_amount,
  }))

  const isDraft = head.status === 0
  const isApproved = head.status === 2
  // A line with no purchase price would book its stock at zero cost. Hidden prices (no cost.view) cannot be judged here.
  const zeroPriceItems = isDraft && !priceBlind
    ? items.filter((it) => it.unit_price !== undefined && !(Number(it.unit_price) > 0))
    : []
  const zeroPriceNames = zeroPriceItems.map((it) => productNames[it.product_id] || `第 ${it.line_no} 行商品`).join("、")
  const totalAmount = parseFloat(head.total_amount) || 0
  const paidSoFar = parseFloat(head.paid_amount ?? "0") || 0
  // A return that cancels what is still owed is booked as a credit entry, not as money paid.
  const returnOffset = payments
    .filter((p) => p.pay_type === "credit" && (parseFloat(p.amount) || 0) > 0)
    .reduce((sum, p) => sum + (parseFloat(p.amount) || 0), 0)
  // Without the payment records the offset is unknown, so show the recorded total as is.
  const cashPaid = paymentsFailed ? paidSoFar : Math.max(paidSoFar - returnOffset, 0)
  const payable = Math.max(totalAmount - paidSoFar, 0)
  const returnedLines = items.filter((it) => (parseFloat(it.returned_qty ?? "0") || 0) > 0)
  // Returns to a supplier (and sales returns) share this table but are not purchases: read-only.
  const isReturnDoc = head.sub_type === "采购退货" || head.sub_type === "销售退货"
  const canReturn = mayReturn && !isReturnDoc && isApproved && items.some((it) => returnablePurchaseQty(it) > 0)

  // Parks the bill's content for the new-bill page; nothing is saved here.
  // Names still loading are fetched here so the new page never opens with blank product labels.
  async function handleCopy() {
    setCopying(true)
    const resolved: Record<string, string> = { ...productNames }
    await Promise.all(
      Array.from(new Set(items.map((it) => it.product_id)))
        .filter((id) => id && !resolved[id])
        .map((id) =>
          getProduct(id, tenantId).then(
            (p) => {
              resolved[id] = p.name
            },
            () => {}
          )
        )
    )
    const money = head as { currency?: string; exchange_rate?: string }
    saveBillCopy("purchase", {
      partner_id: head.partner_id,
      partner_name: names.partnerName || head.partner_name,
      warehouse_id: head.warehouse_id,
      remark: head.remark,
      shipping_fee: head.shipping_fee,
      tax_amount: head.tax_amount,
      // Foreign-currency bills keep their currency and rate, so prices stay in the original currency.
      currency: money.currency && money.currency !== "CNY" ? money.currency : undefined,
      exchange_rate: money.currency && money.currency !== "CNY" ? money.exchange_rate : undefined,
      items: items.map((it) => ({
        product_id: it.product_id,
        product_name: resolved[it.product_id],
        unit_id: it.unit_id,
        unit_name: it.unit_name,
        qty: it.qty,
        unit_price: it.unit_price,
      })),
    })
    router.push("/purchases/new")
  }
  const mayCopy = can("bill.draft")
  const copyButton = mayCopy && (
    <Button variant="outline" disabled={acting || copying} onClick={handleCopy}>
      {copying ? "正在准备…" : "复制为新单"}
    </Button>
  )
  const printButton = (
    <Button variant="outline" onClick={() => window.open(`/print/purchase/${id}`, "_blank")}>
      打印
    </Button>
  )

  return (
    <PageContainer width="wide">
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span className="font-mono">{head.bill_no}</span>
            <Badge tone={BILL_STATUS_TONE[head.status]}>{BILL_STATUS_LABEL[head.status]}</Badge>
            {isApproved && <Badge tone="ok">{isReturnDoc ? "退货单，仅供查看" : "已审核，无法修改"}</Badge>}
          </span>
        }
        subtitle={
          <>
            单据日期：{formatDate(head.bill_date)}
            {head.approved_at && `　审核时间：${formatDateTime(head.approved_at)}`}
          </>
        }
        actions={
          priceBlind ? (
            isDraft && mayEdit ? (
              <Button variant="outline" disabled={acting} onClick={() => router.push(`/purchases/new?edit=${id}`)}>
                修改
              </Button>
            ) : undefined
          ) : isReturnDoc ? printButton : canReturn ? (
            <>
              {printButton}
              {copyButton}
              <Button variant="outline" disabled={acting || returning} onClick={() => { setReturnError(null); setShowReturn(true) }}>
                退货
              </Button>
            </>
          ) : isDraft ? (
            <>
              {printButton}
              {copyButton}
              {mayEdit && (
                <Button variant="outline" disabled={acting} onClick={() => router.push(`/purchases/new?edit=${id}`)}>
                修改
              </Button>
              )}
              {mayCancel && (
                <Button variant="outline" disabled={acting} onClick={handleCancel}>
                取消单据
              </Button>
              )}
              {mayApprove && (
                <Button disabled={acting} onClick={handleApprove}>
                审核通过
              </Button>
              )}
            </>
          ) : (
            <>
              {printButton}
              {copyButton}
            </>
          )
        }
      />

      <div className="space-y-6">
        {actionError && <ErrorBanner>{actionError}</ErrorBanner>}
        {zeroPriceItems.length > 0 && (
          <div role="status" className="rounded-xl border border-warning/40 bg-warning/10 p-3 text-sm">
            待补进货价 {zeroPriceItems.length} 行：{zeroPriceNames}。审核前请先补上价格；如果是赠品，审核时可确认按 0 入账。
          </div>
        )}
        {returnNotice && (
          <div role="status" className="rounded-xl border border-border bg-card p-3 text-sm text-success">
            {returnNotice}
            {returnBillId && (
              <Button variant="outline" size="sm" className="ml-3" onClick={() => window.open(`/print/purchase/${returnBillId}`, "_blank")}>
                打印退货单
              </Button>
            )}
          </div>
        )}

        {/* Meta info */}
        <div className="grid grid-cols-2 gap-4 rounded-xl border border-border bg-card p-4 text-sm sm:grid-cols-3">
          <div>
            <p className="text-muted-foreground">单据类型</p>
            <p className="font-medium">{head.sub_type}</p>
          </div>
          <div>
            <p className="text-muted-foreground">供应商</p>
            <p className="font-medium">{names.partnerName || (head.partner_id ? "已指定" : "未指定")}</p>
          </div>
          <div>
            <p className="text-muted-foreground">仓库</p>
            <p className="font-medium">
              {names.warehouseName ||
                (head.warehouse_id ? "已指定" : isDraft ? "未指定仓库，审核时将使用默认仓" : "未指定")}
            </p>
          </div>
          {!priceBlind && (
            <div>
              <p className="text-muted-foreground">总金额</p>
              <p className="font-mono font-medium tabular-nums">{formatCNY(totalAmount)}</p>
            </div>
          )}
          {isApproved && !isReturnDoc && !priceBlind && (
            <>
              <div>
                <p className="text-muted-foreground">{paymentsFailed ? "已付（可能含退货冲抵）" : "已付"}</p>
                <p className={cn("font-mono font-medium tabular-nums", cashPaid > 0 ? "text-success" : "text-muted-foreground")}>
                  {formatCNY(cashPaid)}
                </p>
              </div>
              {returnOffset > 0 && (
                <div>
                  <p className="text-muted-foreground">退货冲抵</p>
                  <p className="font-mono font-medium tabular-nums">{formatCNY(returnOffset)}</p>
                </div>
              )}
              <div>
                <p className="text-muted-foreground">待付</p>
                <p className={cn("font-mono font-medium tabular-nums", payable > 0 ? "text-warning" : "text-success")}>
                  {formatCNY(payable)}
                </p>
              </div>
            </>
          )}
          {head.remark && (
            <div className="col-span-2">
              <p className="text-muted-foreground">备注</p>
              <p>{head.remark}</p>
            </div>
          )}
        </div>

        {/* Pay at approval — optional */}
        {mayApprove && isDraft && (
          <div className="space-y-3 rounded-xl border border-border bg-card p-4">
            <h2 className="text-sm font-medium">审核时同时付款（可选）</h2>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="approve-pay-method">付款方式</Label>
                <select
                  id="approve-pay-method"
                  className={SELECT_CLASS}
                  value={payMethod}
                  onChange={(e) => setPayMethod(e.target.value)}
                >
                  <option value="transfer">转账</option>
                  <option value="cash">现金</option>
                  <option value="wechat">微信</option>
                  <option value="alipay">支付宝</option>
                  <option value="card">银行卡</option>
                </select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="approve-paid-amount">
                  本次付款（合计 {formatCNY(totalAmount)}，留空=暂不付款）
                </Label>
                <Input
                  id="approve-paid-amount"
                  type="number"
                  value={paidAmount}
                  placeholder="0.00"
                  min="0"
                  step="0.01"
                  onChange={(e) => setPaidAmount(e.target.value)}
                />
              </div>
            </div>
          </div>
        )}

        {/* Line items (read-only) */}
        <div className="rounded-xl border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">商品明细</h2>
          {priceBlind ? (
            <PurchaseDraftLines items={lineItems} onChange={() => {}} readOnly />
          ) : (
            <BillLineEditor
              items={lineItems}
              onChange={() => {}}
              shippingFee={head.shipping_fee}
              taxAmount={head.tax_amount}
              onShippingFeeChange={() => {}}
              onTaxAmountChange={() => {}}
              readOnly
            />
          )}
        </div>

        {returnedLines.length > 0 && (
          <div className="rounded-xl border border-border bg-card p-4">
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">退货情况</h2>
            <ul className="space-y-1 text-sm">
              {returnedLines.map((it) => (
                <li key={it.id}>
                  {productNames[it.product_id] || "未知商品"}　已退 {it.returned_qty}
                  {it.unit_name ?? ""}（共买入 {it.qty}
                  {it.unit_name ?? ""}）
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* Payments section (approved bills only) */}
        {isApproved && !isReturnDoc && mayRecordPayment && (
          <div className="rounded-xl border border-border bg-card p-4">
            <h2 className="mb-4 text-sm font-medium text-muted-foreground">付款记录</h2>
            {paymentsFailed && (
              <p role="status" className="mb-3 text-sm text-warning">
                付款记录暂时没有读到，上面的已付可能包含退货冲抵，待付金额不受影响，请刷新页面再看明细。
              </p>
            )}
            <PaymentForm
              kind="pay"
              billId={id}
              outstandingAmount={String(payable)}
              payments={payments}
              tenantId={tenantId}
              onSuccess={() => load(undefined, undefined, true)}
            />
          </div>
        )}

        {fetchPayments && (
          <BillInvoices
            kind="purchase"
            billId={head.id}
            partnerId={head.partner_id}
            partnerName={names.partnerName}
            approved={isApproved && !isReturnDoc}
            billNo={head.bill_no}
            billDate={head.bill_date}
            totalAmount={head.total_amount}
          />
        )}

        {roleKnown && can("audit.view") && <AuditTrail resourceId={head.id} />}

        <div className="text-center">
          <button
            type="button"
            onClick={() => router.push("/purchases")}
            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            返回采购单列表
          </button>
        </div>
      </div>

      <PurchaseReturnDialog
        open={showReturn}
        onOpenChange={setShowReturn}
        items={items}
        productNames={productNames}
        exchangeRate={(head as { exchange_rate?: string }).exchange_rate}
        submitting={returning}
        error={returnError}
        onSubmit={handleReturn}
      />
    </PageContainer>
  )
}
