"use client"

import { useState, useCallback } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import {
  getSaleBill,
  approveSaleBill,
  cancelSaleBill,
  type SaleBillDetail,
  type SaleBillHead,
  type Payment,
} from "@/lib/api/sale"
import { BILL_STATUS_TONE, BILL_STATUS_LABEL } from "@/lib/status"
import { SaleLineEditor, type SaleLineItem } from "@/components/sale-line-editor"
import { PaymentForm } from "@/components/payment-form"
import { useConfirm } from "@/hooks/useConfirm"
import { useTenantId } from "@/hooks/use-tenant-id"
import { formatCNY, formatDate, formatDateTime } from "@/lib/format"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

export default function SaleDetailPage() {
  const { id } = useParams<{ id: string }>()
  const router = useRouter()

  const [detail, setDetail] = useState<SaleBillDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [acting, setActing] = useState(false)

  // Quick-approve payment fields
  const [showApproveForm, setShowApproveForm] = useState(false)
  const [paidAmount, setPaidAmount] = useState("")
  const [payMethod, setPayMethod] = useState("cash")

  const confirm = useConfirm()
  const tenantId = useTenantId()

  const load = useCallback((signal?: AbortSignal, isCancelled?: () => boolean) => {
    setLoading(true)
    setError(null)
    getSaleBill(id, tenantId, signal)
      .then((data) => {
        if (isCancelled?.()) return
        setDetail(data)
      })
      .catch((e) => {
        if (isCancelled?.() || signal?.aborted) return
        setError(String(e))
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
    try {
      await approveSaleBill(
        id,
        { paid_amount: paidAmount || undefined, payment_method: payMethod },
        tenantId
      )
      setShowApproveForm(false)
      load()
    } catch (e) {
      setActionError(String(e))
    } finally {
      setActing(false)
    }
  }

  async function handleCancel() {
    const ok = await confirm({ title: "取消此销售单", body: "单据状态将变为已取消，操作不可撤销。", danger: true, confirmText: "确认取消" })
    if (!ok) return
    setActing(true)
    setActionError(null)
    try {
      await cancelSaleBill(id, tenantId)
      load()
    } catch (e) {
      setActionError(String(e))
    } finally {
      setActing(false)
    }
  }

  function handlePaymentRecorded(payment: Payment) {
    if (!detail) return
    const updatedPayments = [...detail.payments, payment]
    const paidSum = updatedPayments.reduce((acc, p) => acc + parseFloat(p.amount), 0)
    const receivable = Math.max(0, parseFloat(detail.head.total_amount) - paidSum)
    const updatedHead: SaleBillHead = {
      ...detail.head,
      paid_amount: String(paidSum.toFixed(4)),
      receivable_amount: String(receivable.toFixed(4)),
    }
    setDetail({ ...detail, head: updatedHead, payments: updatedPayments })
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
    unit_id: it.unit_id,
    unit_name: it.unit_name,
    line_no: it.line_no,
    qty: it.qty,
    unit_price: it.unit_price,
  }))

  const isDraft = head.status === 0
  const isApproved = head.status === 2
  const receivable = parseFloat(head.receivable_amount) || 0

  return (
    <PageContainer width="wide">
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span className="font-mono">{head.bill_no}</span>
            <Badge tone={BILL_STATUS_TONE[head.status]}>{BILL_STATUS_LABEL[head.status]}</Badge>
            {isApproved && <Badge tone="ok">已批准，无法编辑</Badge>}
          </span>
        }
        subtitle={
          <>
            单据日期：{formatDate(head.bill_date)}
            {head.approved_at && `　审核时间：${formatDateTime(head.approved_at)}`}
          </>
        }
        actions={
          isDraft ? (
            <>
              <Button variant="outline" disabled={acting} onClick={handleCancel}>
                取消单据
              </Button>
              <Button disabled={acting} onClick={() => setShowApproveForm(!showApproveForm)}>
                审核出库
              </Button>
            </>
          ) : undefined
        }
      />

      <div className="space-y-6">
        {actionError && <ErrorBanner>{actionError}</ErrorBanner>}

        {/* Approve form — expanded on click */}
        {isDraft && showApproveForm && (
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

        {/* Payments section (approved bills only) */}
        {isApproved && (
          <div className="rounded-xl border border-border bg-card p-4">
            <h2 className="mb-4 text-sm font-medium text-muted-foreground">收款记录</h2>
            <PaymentForm
              billId={id}
              receivableAmount={head.receivable_amount}
              payments={payments}
              tenantId={tenantId}
              onSuccess={handlePaymentRecorded}
            />
          </div>
        )}

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
    </PageContainer>
  )
}
