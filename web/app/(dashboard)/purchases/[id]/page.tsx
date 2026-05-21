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
  type BillDetail,
  BILL_STATUS_LABEL,
} from "@/lib/api/purchase"
import { BILL_STATUS_TONE } from "@/lib/status"
import { globalUndoStack } from "@/lib/undo/undo-stack"
import { BillLineEditor, type BillLineItem } from "@/components/bill-line-editor"
import { useConfirm } from "@/hooks/useConfirm"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Skeleton } from "@/components/ui/skeleton"
import { formatDate, formatDateTime } from "@/lib/format"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

export default function PurchaseDetailPage() {
  const { id } = useParams<{ id: string }>()
  const router = useRouter()

  const [detail, setDetail] = useState<BillDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [acting, setActing] = useState(false)

  const confirm = useConfirm()

  const load = useCallback((signal?: AbortSignal, isCancelled?: () => boolean) => {
    setLoading(true)
    setError(null)
    getPurchaseBill(id, devTenantId, signal)
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
  }, [id])

  useAbortableEffect((signal, isCancelled) => {
    load(signal, isCancelled)
  }, [load])

  async function handleApprove() {
    const ok = await confirm({ title: "审核通过此采购单", body: "审核后不可取消，将自动更新库存。", confirmText: "确认审核" })
    if (!ok) return
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
      globalUndoStack.pop()
      setActionError(String(e))
    } finally {
      setActing(false)
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
    unit_id: it.unit_id,
    unit_name: it.unit_name,
    line_no: it.line_no,
    qty: it.qty,
    unit_price: it.unit_price,
  }))

  const isDraft = head.status === 0
  const isApproved = head.status === 2

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
              <Button disabled={acting} onClick={handleApprove}>
                审核通过
              </Button>
            </>
          ) : undefined
        }
      />

      <div className="space-y-6">
        {actionError && <ErrorBanner>{actionError}</ErrorBanner>}

        {/* Meta info */}
        <div className="grid grid-cols-2 gap-4 rounded-xl border border-border bg-card p-4 text-sm sm:grid-cols-3">
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
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">商品明细</h2>
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
            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            返回采购单列表
          </button>
        </div>
      </div>
    </PageContainer>
  )
}
