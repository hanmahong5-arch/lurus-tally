"use client"

import { useEffect, useState, useCallback } from "react"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import {
  getPurchaseBill,
  approvePurchaseBill,
  cancelPurchaseBill,
  restorePurchaseBill,
  type BillDetail,
  type BillStatus,
  BILL_STATUS_LABEL,
} from "@/lib/api/purchase"
import { globalUndoStack } from "@/lib/undo/undo-stack"
import { BillLineEditor, type BillLineItem } from "@/components/bill-line-editor"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

const STATUS_BADGE: Record<BillStatus, string> = {
  0: "bg-muted text-muted-foreground",
  2: "bg-green-500/10 text-green-600",
  9: "bg-red-500/10 text-red-500",
}

export default function PurchaseDetailPage() {
  const { id } = useParams<{ id: string }>()
  const router = useRouter()

  const [detail, setDetail] = useState<BillDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [acting, setActing] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    getPurchaseBill(id, devTenantId)
      .then(setDetail)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [id])

  useEffect(() => {
    load()
  }, [load])

  async function handleApprove() {
    if (!confirm("确认审核通过此采购单？审核后不可取消。")) return
    setActing(true)
    setActionError(null)
    try {
      await approvePurchaseBill(id, devTenantId)
      load()
    } catch (e) {
      setActionError(String(e))
    } finally {
      setActing(false)
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
        await restorePurchaseBill(id, devTenantId)
        load()
      },
    })

    setActing(true)
    setActionError(null)
    try {
      await cancelPurchaseBill(id, devTenantId)
      load()
    } catch (e) {
      // Cancel failed — remove the undo entry we just pushed.
      globalUndoStack.pop()
      setActionError(String(e))
    } finally {
      setActing(false)
    }
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
          {error ?? "采购单不存在"}
        </div>
        <Link href="/purchases" className="text-sm text-primary hover:underline">
          返回列表
        </Link>
      </div>
    )
  }

  const { head, items } = detail
  const lineItems: BillLineItem[] = items.map((it) => ({
    product_id: it.product_id,
    unit_id: it.unit_id,
    unit_name: it.unit_name,
    line_no: it.line_no,
    qty: it.qty,
    unit_price: it.unit_price,
  }))

  const isDraft = head.status === 0

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
              `　 审核时间：${new Date(head.approved_at).toLocaleString("zh-CN")}`}
          </p>
        </div>

        {isDraft && (
          <div className="flex gap-2 flex-shrink-0">
            <button
              disabled={acting}
              onClick={handleCancel}
              className="rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-muted transition-colors disabled:opacity-60"
            >
              取消单据
            </button>
            <button
              disabled={acting}
              onClick={handleApprove}
              className="rounded-lg bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-60"
            >
              审核通过
            </button>
          </div>
        )}
      </div>

      {actionError && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {actionError}
        </div>
      )}

      {/* Meta info */}
      <div className="rounded-xl border border-border bg-card p-4 grid grid-cols-2 sm:grid-cols-3 gap-4 text-sm">
        <div>
          <p className="text-muted-foreground">单据类型</p>
          <p className="font-medium">{head.sub_type}</p>
        </div>
        {head.remark && (
          <div className="col-span-2">
            <p className="text-muted-foreground">备注</p>
            <p>{head.remark}</p>
          </div>
        )}
      </div>

      {/* Line items (read-only) */}
      <div className="rounded-xl border border-border bg-card p-4">
        <h2 className="text-sm font-medium text-muted-foreground mb-3">商品明细</h2>
        <BillLineEditor
          items={lineItems}
          onChange={() => {}}
          shippingFee={head.shipping_fee}
          taxAmount={head.tax_amount}
          onShippingFeeChange={() => {}}
          onTaxAmountChange={() => {}}
          readOnly
        />
      </div>

      <div className="text-center">
        <button
          type="button"
          onClick={() => router.push("/purchases")}
          className="text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          返回采购单列表
        </button>
      </div>
    </div>
  )
}
