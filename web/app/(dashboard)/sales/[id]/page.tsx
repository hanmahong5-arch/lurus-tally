"use client"

import { useEffect, useState, useCallback } from "react"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import {
  getSaleBill,
  approveSaleBill,
  cancelSaleBill,
  type SaleBillDetail,
  type SaleBillHead,
  type Payment,
  type BillStatus,
} from "@/lib/api/sale"
import { SaleLineEditor, type SaleLineItem } from "@/components/sale-line-editor"
import { PaymentForm } from "@/components/payment-form"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const BILL_STATUS_LABEL: Record<BillStatus, string> = {
  0: "草稿",
  2: "已审核",
  9: "已取消",
}

// draft=gray, approved=blue, cancelled=red
const STATUS_BADGE: Record<BillStatus, string> = {
  0: "bg-muted text-muted-foreground",
  2: "bg-blue-500/10 text-blue-600",
  9: "bg-red-500/10 text-red-500",
}

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

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    getSaleBill(id, devTenantId)
      .then(setDetail)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [id])

  useEffect(() => {
    load()
  }, [load])

  async function handleApprove() {
    setActing(true)
    setActionError(null)
    try {
      await approveSaleBill(
        id,
        {
          paid_amount: paidAmount || undefined,
          payment_method: payMethod,
        },
        devTenantId
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
    if (!confirm("确认取消此销售单？")) return
    setActing(true)
    setActionError(null)
    try {
      await cancelSaleBill(id, devTenantId)
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
    const paidSum = updatedPayments.reduce(
      (acc, p) => acc + parseFloat(p.amount),
      0
    )
    const receivable = Math.max(
      0,
      parseFloat(detail.head.total_amount) - paidSum
    )
    const updatedHead: SaleBillHead = {
      ...detail.head,
      paid_amount: String(paidSum.toFixed(4)),
      receivable_amount: String(receivable.toFixed(4)),
    }
    setDetail({ ...detail, head: updatedHead, payments: updatedPayments })
  }

  if (loading) {
    return (
      <div className="p-6 text-center text-muted-foreground">加载中...</div>
    )
  }

  if (error || !detail) {
    return (
      <div className="p-6">
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive mb-4">
          {error ?? "销售单不存在"}
        </div>
        <Link href="/sales" className="text-sm text-primary hover:underline">
          返回列表
        </Link>
      </div>
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
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      {/* Header row */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-semibold font-mono">{head.bill_no}</h1>
            <span
              className={`rounded-full px-2 py-0.5 text-xs ${STATUS_BADGE[head.status]}`}
            >
              {BILL_STATUS_LABEL[head.status]}
            </span>
          </div>
          <p className="text-sm text-muted-foreground mt-0.5">
            单据日期：{new Date(head.bill_date).toLocaleDateString("zh-CN")}
            {head.approved_at &&
              `　审核时间：${new Date(head.approved_at).toLocaleString("zh-CN")}`}
          </p>
        </div>

        <div className="flex gap-2 flex-shrink-0">
          {isDraft && (
            <>
              <button
                disabled={acting}
                onClick={handleCancel}
                className="rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-muted transition-colors disabled:opacity-60"
              >
                取消单据
              </button>
              <button
                disabled={acting}
                onClick={() => setShowApproveForm(!showApproveForm)}
                className="rounded-lg bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-60"
              >
                审核出库
              </button>
            </>
          )}
        </div>
      </div>

      {actionError && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {actionError}
        </div>
      )}

      {/* Approve form — expanded on click */}
      {isDraft && showApproveForm && (
        <div className="rounded-xl border border-border bg-card p-4 space-y-3">
          <h2 className="text-sm font-medium">审核并收款（可选）</h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">支付方式</label>
              <select
                className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"
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
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">
                实收金额（应收 ¥{parseFloat(head.total_amount).toFixed(2)}，留空=0）
              </label>
              <input
                type="number"
                className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"
                value={paidAmount}
                placeholder="0.00"
                min="0"
                step="0.01"
                onChange={(e) => setPaidAmount(e.target.value)}
              />
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setShowApproveForm(false)}
              className="rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-muted transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              disabled={acting}
              onClick={handleApprove}
              className="rounded-lg bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-60"
            >
              {acting ? "处理中..." : "确认审核"}
            </button>
          </div>
        </div>
      )}

      {/* Meta info */}
      <div className="rounded-xl border border-border bg-card p-4 grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
        <div>
          <p className="text-muted-foreground">单据类型</p>
          <p className="font-medium">{head.sub_type}</p>
        </div>
        <div>
          <p className="text-muted-foreground">总金额</p>
          <p className="font-mono font-medium">¥ {parseFloat(head.total_amount).toFixed(2)}</p>
        </div>
        <div>
          <p className="text-muted-foreground">已收</p>
          <p className="font-mono font-medium text-green-600">
            ¥ {parseFloat(head.paid_amount).toFixed(2)}
          </p>
        </div>
        <div>
          <p className="text-muted-foreground">应收</p>
          <p
            className={`font-mono font-medium ${
              receivable > 0 ? "text-amber-600" : "text-green-600"
            }`}
          >
            ¥ {receivable.toFixed(2)}
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
        <h2 className="text-sm font-medium text-muted-foreground mb-3">商品明细</h2>
        <SaleLineEditor items={lineItems} onChange={() => {}} readOnly />
      </div>

      {/* Payments section (approved bills only) */}
      {isApproved && (
        <div className="rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium text-muted-foreground mb-4">收款记录</h2>
          <PaymentForm
            billId={id}
            receivableAmount={head.receivable_amount}
            payments={payments}
            tenantId={devTenantId}
            onSuccess={handlePaymentRecorded}
          />
        </div>
      )}

      <div className="text-center">
        <button
          type="button"
          onClick={() => router.push("/sales")}
          className="text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          返回销售单列表
        </button>
      </div>
    </div>
  )
}
