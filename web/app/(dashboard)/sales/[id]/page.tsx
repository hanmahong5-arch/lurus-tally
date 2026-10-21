"use client"

import { useState, useCallback, useEffect } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import {
  getSaleBill,
  approveSaleBill,
  cancelSaleBill,
  returnSaleBill,
  type SaleBillDetail,
} from "@/lib/api/sale"
import { BILL_STATUS_TONE, BILL_STATUS_LABEL } from "@/lib/status"
import { SaleLineEditor, type SaleLineItem } from "@/components/sale-line-editor"
import { PaymentForm } from "@/components/payment-form"
import { RefundRetryModal, SaleReturnDialog, returnableQty, type PendingRefund, type RefundChoice } from "@/components/sale-return-dialog"
import { useSubmitKey } from "@/lib/submit-key"
import { useConfirm } from "@/hooks/useConfirm"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useRole } from "@/hooks/use-role"
import { useProductNames } from "@/hooks/use-product-names"
import { useBillPartyNames } from "@/hooks/use-bill-party-names"
import { formatCNY, formatDate, formatDateTime } from "@/lib/format"
import { BillInvoices } from "@/components/bill-invoices"
import { AuditTrail } from "@/components/audit-trail"
import { CloudPrintButton } from "@/components/print/cloud-print-button"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import { extractApiError } from "@/lib/api/errors"
import { getProduct } from "@/lib/api/products"
import { forgetPendingRefund, readPendingRefund, rememberPendingRefund } from "@/lib/pos/pending-refund"
import { saveBillCopy } from "@/lib/bill-copy"
import { describeStockShortage } from "@/lib/stock-shortage"

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

export default function SaleDetailPage() {
  const { id } = useParams<{ id: string }>()
  const router = useRouter()

  const [detail, setDetail] = useState<SaleBillDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  // Set when the approve failed on stock, so the banner offers the way out.
  const [shortStock, setShortStock] = useState(false)
  const [acting, setActing] = useState(false)

  // Customer return dialog
  const [showReturn, setShowReturn] = useState(false)
  const [returning, setReturning] = useState(false)
  const [returnError, setReturnError] = useState<string | null>(null)
  const [returnNotice, setReturnNotice] = useState<string | null>(null)
  const [returnBillId, setReturnBillId] = useState<string | null>(null)
  const returnKey = useSubmitKey()

  // A return whose refund did not get booked; stays until 补记退款 succeeds.
  const refundKey = useSubmitKey()
  const [pendingRefund, setPendingRefund] = useState<PendingRefund | null>(null)
  const [refundModalOpen, setRefundModalOpen] = useState(false)
  const [recordingRefund, setRecordingRefund] = useState(false)
  const [refundError, setRefundError] = useState<string | null>(null)

  useEffect(() => {
    setPendingRefund(readPendingRefund(id))
  }, [id])

  // Quick-approve payment fields
  const [showApproveForm, setShowApproveForm] = useState(false)
  const [paidAmount, setPaidAmount] = useState("")
  const [payMethod, setPayMethod] = useState("cash")

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
  // The invoice panel asks the backend on mount, so only a role allowed to read payments mounts it.
  const showInvoices = roleKnown && can("payment.read")
  const productNames = useProductNames(detail?.items.map((it) => it.product_id) ?? [], tenantId)
  const names = useBillPartyNames({
    kind: "customer",
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
    return getSaleBill(id, tenantId, signal)
      .then((data) => {
        if (isCancelled?.()) return
        setDetail(data)
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
  }, [id, tenantId])

  useAbortableEffect((signal, isCancelled) => {
    load(signal, isCancelled)
  }, [load])

  async function handleApprove() {
    setActing(true)
    setActionError(null)
    setShortStock(false)
    try {
      await approveSaleBill(
        id,
        { paid_amount: paidAmount || undefined, payment_method: payMethod },
        tenantId
      )
      setShowApproveForm(false)
      load()
    } catch (e) {
      const shortage = await describeStockShortage(e, tenantId, productNames)
      setShortStock(shortage !== null)
      setActionError(shortage ?? extractApiError(e).message)
    } finally {
      setActing(false)
    }
  }

  async function handleCancel() {
    const ok = await confirm({ title: "取消此销售单", body: "单据状态将变为已取消，操作不可撤销。", danger: true, confirmText: "确认取消" })
    if (!ok) return
    setActing(true)
    setActionError(null)
    setShortStock(false)
    try {
      await cancelSaleBill(id, tenantId)
      load()
    } catch (e) {
      setShortStock(false)
      setActionError(extractApiError(e).message)
    } finally {
      setActing(false)
    }
  }

  async function handleReturn(lines: { bill_item_id: string; qty: string }[], remark: string, refund?: RefundChoice) {
    const body = {
      items: lines,
      remark: remark || undefined,
      refund_amount: refund?.amount,
      refund_method: refund?.method,
    }
    setReturning(true)
    setReturnError(null)
    try {
      const res = await returnSaleBill(id, body, tenantId, returnKey.keyFor(body))
      returnKey.succeeded()
      setShowReturn(false)
      const refunded = parseFloat(res.refunded_amount ?? "0") || 0
      setReturnNotice(
        `退货成功，退货单号 ${res.bill_no}，退货金额 ${formatCNY(parseFloat(res.return_amount))}` +
          (refunded > 0 ? `，已退款 ${formatCNY(refunded)}` : "")
      )
      setReturnBillId(res.bill_id)
      if (res.refund_error) {
        const pending = { saleId: id, returnBillId: res.bill_id, amount: refund?.amount ?? "0", method: refund?.method ?? "cash" }
        rememberPendingRefund(pending)
        setPendingRefund(pending)
        setRefundModalOpen(true)
        setShortStock(false)
        setActionError(res.refund_error)
      }
      load()
    } catch (e) {
      returnKey.failed(e)
      setReturnError(extractApiError(e).message)
    } finally {
      setReturning(false)
    }
  }

  async function recordRefund(p: PendingRefund) {
    const body = { items: [], refund_only: true, refund_amount: p.amount, refund_method: p.method, return_bill_id: p.returnBillId }
    setRecordingRefund(true)
    setRefundError(null)
    try {
      await returnSaleBill(p.saleId, body, tenantId, refundKey.keyFor(body))
      refundKey.succeeded()
      forgetPendingRefund(p.saleId)
      setPendingRefund(null)
      setRefundModalOpen(false)
      setActionError(null)
      setReturnNotice(`已补记退款 ${formatCNY(parseFloat(p.amount) || 0)}`)
      load(undefined, undefined, true)
    } catch (e) {
      refundKey.failed(e)
      setRefundError(extractApiError(e).message)
    } finally {
      setRecordingRefund(false)
    }
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
          <ErrorBanner hint="请刷新页面重试">{error ?? "销售单不存在"}</ErrorBanner>
          <Link href="/sales" className="text-sm text-primary hover:underline">
            返回列表
          </Link>
        </div>
      </PageContainer>
    )
  }

  const { head, items, payments } = detail
  const lineItems: SaleLineItem[] = items.map((it) => ({
    product_id: it.product_id,
    product_name: productNames[it.product_id],
    unit_id: it.unit_id,
    unit_name: it.unit_name,
    line_no: it.line_no,
    qty: it.qty,
    unit_price: it.unit_price,
    _subtotal: it.line_amount,
  }))

  const isDraft = head.status === 0
  const isApproved = head.status === 2

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
    saveBillCopy("sale", {
      partner_id: head.partner_id,
      partner_name: names.partnerName || head.partner_name,
      warehouse_id: head.warehouse_id,
      remark: head.remark,
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
    router.push("/sales/new")
  }
  const mayCopy = can("bill.draft") || can("bill.checkout")
  const copyButton = mayCopy && (
    <Button variant="outline" disabled={acting || copying} onClick={handleCopy}>
      {copying ? "正在准备…" : "复制为新单"}
    </Button>
  )
  const printButton = (
    <>
      <Button variant="outline" onClick={() => window.open(`/print/sale/${id}`, "_blank")}>
        打印
      </Button>
      <CloudPrintButton billId={id} className="h-9 px-4 text-sm" />
    </>
  )
  // The detail endpoint reports the post-return figure at the top level; head only has it on lists.
  const receivableAmount = detail.receivable_amount ?? head.receivable_amount
  const receivable = parseFloat(receivableAmount) || 0
  const returnedAmount = parseFloat(detail.returned_amount ?? "0") || 0
  const refundDue = parseFloat(detail.refund_due_amount ?? "0") || 0
  const returnedLines = items.filter((it) => (parseFloat(it.returned_qty ?? "0") || 0) > 0)
  const canReturn = mayReturn && isApproved && items.some((it) => returnableQty(it) > 0)
  // The customer is paid back the way they paid: the first real (non-credit) payment on the sale.
  const refundDefaultMethod = payments.find((p) => p.pay_type !== "credit" && (parseFloat(p.amount) || 0) > 0)?.pay_type ?? "cash"

  return (
    <PageContainer width="wide">
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span className="font-mono">{head.bill_no}</span>
            <Badge tone={BILL_STATUS_TONE[head.status]}>{BILL_STATUS_LABEL[head.status]}</Badge>
            {isApproved && <Badge tone="ok">已审核，无法修改</Badge>}
          </span>
        }
        subtitle={
          <>
            单据日期：{formatDate(head.bill_date)}
            {head.approved_at && `　审核时间：${formatDateTime(head.approved_at)}`}
          </>
        }
        actions={
          canReturn ? (
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
                <Button variant="outline" disabled={acting} onClick={() => router.push(`/sales/new?edit=${id}`)}>
                修改
              </Button>
              )}
              {mayCancel && (
                <Button variant="outline" disabled={acting} onClick={handleCancel}>
                取消单据
              </Button>
              )}
              {mayApprove && (
                <Button disabled={acting} onClick={() => setShowApproveForm(!showApproveForm)}>
                审核出库
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
        {actionError && (
          <ErrorBanner
            hint={
              shortStock && (
                <Link href="/stock" className="underline">
                  去盘点或进货
                </Link>
              )
            }
          >
            {actionError}
          </ErrorBanner>
        )}
        {pendingRefund && (
          <div role="status" className="rounded-xl border border-border bg-card p-3 text-sm">
            有一笔退款 {formatCNY(parseFloat(pendingRefund.amount) || 0)} 还没记账，今日结单里还没有这笔钱。
            <Button size="sm" className="ml-3" onClick={() => { setRefundError(null); setRefundModalOpen(true) }}>
              补记退款
            </Button>
          </div>
        )}
        {returnNotice && (
          <div role="status" className="rounded-xl border border-border bg-card p-3 text-sm text-success">
            {returnNotice}
            {returnBillId && (
              <Button variant="outline" size="sm" className="ml-3" onClick={() => window.open(`/print/sale/${returnBillId}`, "_blank")}>
                打印退货单
              </Button>
            )}
          </div>
        )}

        {/* Approve form — expanded on click */}
        {mayApprove && isDraft && showApproveForm && (
          <div className="space-y-3 rounded-xl border border-border bg-card p-4">
            <h2 className="text-sm font-medium">审核并收款（可选）</h2>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="approve-pay-method">支付方式</Label>
                <select
                  id="approve-pay-method"
                  className={SELECT_CLASS}
                  value={payMethod}
                  onChange={(e) => setPayMethod(e.target.value)}
                >
                  <option value="cash">现金</option>
                  <option value="wechat">微信</option>
                  <option value="alipay">支付宝</option>
                  <option value="card">银行卡</option>
                  <option value="transfer">转账</option>
                </select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="approve-paid-amount">
                  实收金额（应收 {formatCNY(parseFloat(head.total_amount))}，留空=0）
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
            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setShowApproveForm(false)}>
                取消
              </Button>
              <Button size="sm" disabled={acting} onClick={handleApprove}>
                {acting ? "处理中..." : "确认审核"}
              </Button>
            </div>
          </div>
        )}

        {/* Meta info */}
        <div className="grid grid-cols-2 gap-4 rounded-xl border border-border bg-card p-4 text-sm sm:grid-cols-4">
          <div>
            <p className="text-muted-foreground">单据类型</p>
            <p className="font-medium">{head.sub_type}</p>
          </div>
          <div>
            <p className="text-muted-foreground">客户</p>
            <p className="font-medium">{names.partnerName || (head.partner_id ? "已指定" : "未指定")}</p>
          </div>
          <div>
            <p className="text-muted-foreground">仓库</p>
            <p className="font-medium">
              {names.warehouseName ||
                (head.warehouse_id ? "已指定" : isDraft ? "未指定仓库，审核时将使用默认仓" : "未指定")}
            </p>
          </div>
          <div>
            <p className="text-muted-foreground">总金额</p>
            <p className="font-mono font-medium tabular-nums">{formatCNY(parseFloat(head.total_amount))}</p>
          </div>
          <div>
            <p className="text-muted-foreground">已收</p>
            <p className="font-mono font-medium tabular-nums text-success">
              {formatCNY(parseFloat(head.paid_amount))}
            </p>
          </div>
          <div>
            <p className="text-muted-foreground">应收</p>
            <p className={cn("font-mono font-medium tabular-nums", receivable > 0 ? "text-warning" : "text-success")}>
              {formatCNY(receivable)}
            </p>
          </div>
          {returnedAmount > 0 && (
            <div>
              <p className="text-muted-foreground">已退货金额</p>
              <p className="font-mono font-medium tabular-nums">{formatCNY(returnedAmount)}</p>
            </div>
          )}
          {refundDue > 0 && (
            <div className="col-span-2 sm:col-span-4">
              <p className="text-muted-foreground">应退给客户</p>
              <p>
                <span className="font-mono font-medium tabular-nums text-warning">{formatCNY(refundDue)}</span>
                　客户已付的钱超出了退货后的金额，这部分钱系统不会自动退，请当面退给客户。
              </p>
            </div>
          )}
          {head.remark && (
            <div className="col-span-2 sm:col-span-4">
              <p className="text-muted-foreground">备注</p>
              <p>{head.remark}</p>
            </div>
          )}
        </div>

        {/* Line items (read-only) */}
        <div className="rounded-xl border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">商品明细</h2>
          <SaleLineEditor items={lineItems} onChange={() => {}} readOnly />
        </div>

        {returnedLines.length > 0 && (
          <div className="rounded-xl border border-border bg-card p-4">
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">退货情况</h2>
            <ul className="space-y-1 text-sm">
              {returnedLines.map((it) => (
                <li key={it.id}>
                  {productNames[it.product_id] || "未知商品"}　已退 {it.returned_qty}
                  {it.unit_name ?? ""}（共卖出 {it.qty}
                  {it.unit_name ?? ""}）
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* Payments section (approved bills only) */}
        {isApproved && mayRecordPayment && (
          <div className="rounded-xl border border-border bg-card p-4">
            <h2 className="mb-4 text-sm font-medium text-muted-foreground">收款记录</h2>
            <PaymentForm
              billId={id}
              outstandingAmount={receivableAmount}
              payments={payments}
              tenantId={tenantId}
              onSuccess={() => load(undefined, undefined, true)}
            />
          </div>
        )}

        <RefundRetryModal
          pending={refundModalOpen ? pendingRefund : null}
          submitting={recordingRefund}
          error={refundError}
          onRecord={recordRefund}
          onClose={() => setRefundModalOpen(false)}
        />

        {showInvoices && (
          <BillInvoices
            kind="sale"
            billId={head.id}
            partnerId={head.partner_id}
            partnerName={names.partnerName}
            approved={isApproved}
            billNo={head.bill_no}
            billDate={head.bill_date}
            totalAmount={head.total_amount}
            returnedAmount={detail.returned_amount}
          />
        )}

        {roleKnown && can("audit.view") && <AuditTrail resourceId={head.id} />}

        <div className="text-center">
          <button
            type="button"
            onClick={() => router.push("/sales")}
            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            返回销售单列表
          </button>
        </div>
      </div>

      <SaleReturnDialog
        open={showReturn}
        onOpenChange={setShowReturn}
        items={items}
        productNames={productNames}
        submitting={returning}
        error={returnError}
        refund={{
          total: head.total_amount,
          paid: head.paid_amount,
          returnedAmount: detail.returned_amount ?? "0",
          defaultMethod: refundDefaultMethod,
        }}
        onSubmit={handleReturn}
      />
    </PageContainer>
  )
}
