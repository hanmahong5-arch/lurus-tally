"use client"

import { useCallback, useEffect, useState } from "react"
import {
  getSaleBill,
  listSaleBills,
  returnSaleBill,
  type ReturnSaleRequest,
  type SaleBillDetail,
  type SaleBillHead,
} from "@/lib/api/sale"
import { forgetPendingRefund, rememberPendingRefund } from "@/lib/pos/pending-refund"
import { RefundRetryModal, SaleReturnDialog, type PendingRefund, type RefundChoice } from "@/components/sale-return-dialog"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ErrorBanner } from "@/components/ui/error-banner"
import { useRole } from "@/hooks/use-role"
import { LOCKED_HINT, useSubscriptionLocked } from "@/hooks/use-subscription-locked"
import { beijingToday } from "@/lib/api/pos"
import { useProductNames } from "@/hooks/use-product-names"
import { useSubmitKey } from "@/lib/submit-key"
import { extractApiError } from "@/lib/api/errors"
import { formatCNY, formatDateTime } from "@/lib/format"

export interface PosReturnProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  tenantId?: string
  /** Called with a one-line result for the till to show once the return went through. */
  onDone: (message: string) => void
}

/**
 * Customer return at the till: find the original receipt by its number or among the latest
 * 20 sales, pick what comes back and (optionally) hand the money back right away.
 * Without the right to return any sale (a cashier) only today's receipts the person rang
 * up themselves are listed; everything else goes to the boss or manager.
 */
export function PosReturn({ open, onOpenChange, tenantId, onDone }: PosReturnProps) {
  const locked = useSubscriptionLocked()
  const { known, can } = useRole()
  const ownOnly = known && !can("bill.return")
  const noReturn = known && !can("bill.return.own")
  const [keyword, setKeyword] = useState("")
  const [bills, setBills] = useState<SaleBillHead[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [picked, setPicked] = useState<SaleBillDetail | null>(null)
  const [opening, setOpening] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [returnError, setReturnError] = useState<string | null>(null)
  const returnKey = useSubmitKey()
  const refundKey = useSubmitKey()
  const [pendingRefund, setPendingRefund] = useState<PendingRefund | null>(null)
  const [refundError, setRefundError] = useState<string | null>(null)
  const [recording, setRecording] = useState(false)
  const productNames = useProductNames(picked?.items.map((it) => it.product_id) ?? [], tenantId)

  const search = useCallback(
    (q: string) => {
      setLoading(true)
      setError(null)
      // Only approved sales have anything to give back.
      const scope = ownOnly ? { mine: true, date_from: beijingToday(), date_to: beijingToday() } : {}
      listSaleBills({ page: 1, size: 20, status: 2, q: q || undefined, tenantId, ...scope })
        .then((res) => setBills(res.items ?? []))
        .catch((e) => setError(extractApiError(e).message))
        .finally(() => setLoading(false))
    },
    [tenantId, ownOnly]
  )

  useEffect(() => {
    if (!open || noReturn) return
    setKeyword("")
    setPicked(null)
    setPendingRefund(null)
    setReturnError(null)
    search("")
  }, [open, noReturn, search])

  async function pick(bill: SaleBillHead) {
    setOpening(bill.id)
    setError(null)
    try {
      const detail = await getSaleBill(bill.id, tenantId)
      setReturnError(null)
      setPicked(detail)
    } catch (e) {
      setError(extractApiError(e).message)
    } finally {
      setOpening(null)
    }
  }

  async function submit(lines: { bill_item_id: string; qty: string }[], remark: string, refund?: RefundChoice) {
    if (!picked) return
    if (locked) {
      setReturnError(LOCKED_HINT)
      return
    }
    const body = {
      items: lines,
      remark: remark || undefined,
      refund_amount: refund?.amount,
      refund_method: refund?.method,
    }
    setSubmitting(true)
    setReturnError(null)
    try {
      const res = await returnSaleBill(picked.head.id, body, tenantId, returnKey.keyFor(body))
      returnKey.succeeded()
      const refunded = parseFloat(res.refunded_amount ?? "0") || 0
      let msg = `退货成功，退货金额 ${formatCNY(parseFloat(res.return_amount))}`
      if (refunded > 0) msg += `，已退款 ${formatCNY(refunded)}`
      if (res.refund_error) {
        // The goods are back but the money is not booked: keep the till on this step until it is.
        const pending = { saleId: picked.head.id, returnBillId: res.bill_id, amount: refund?.amount ?? "0", method: refund?.method ?? "cash" }
        rememberPendingRefund(pending)
        setPendingRefund(pending)
        setRefundError(null)
        setPicked(null)
        return
      }
      onDone(msg)
      setPicked(null)
      onOpenChange(false)
    } catch (e) {
      returnKey.failed(e)
      setReturnError(extractApiError(e).message)
    } finally {
      setSubmitting(false)
    }
  }

  async function recordRefund(p: PendingRefund) {
    const body: ReturnSaleRequest = {
      items: [],
      refund_only: true,
      refund_amount: p.amount,
      refund_method: p.method,
      return_bill_id: p.returnBillId,
    }
    setRecording(true)
    setRefundError(null)
    try {
      await returnSaleBill(p.saleId, body, tenantId, refundKey.keyFor(body))
      refundKey.succeeded()
      forgetPendingRefund(p.saleId)
      setPendingRefund(null)
      onDone(`退货成功，已补记退款 ${formatCNY(parseFloat(p.amount) || 0)}`)
      onOpenChange(false)
    } catch (e) {
      refundKey.failed(e)
      setRefundError(extractApiError(e).message)
    } finally {
      setRecording(false)
    }
  }

  function leaveRefund() {
    setPendingRefund(null)
    onDone(
      can("bill.return")
        ? "货已退回入库，但退款没有记上，今日结单里还没有这笔钱。可以到这张小票详情页点「补记退款」，或请老板、店长补记"
        : "货已退回入库，但退款没有记上，今日结单里还没有这笔钱。请把这张小票交给老板，或请老板、店长补记退款",
    )
    onOpenChange(false)
  }

  const refundMethod =
    picked?.payments.find((p) => p.pay_type !== "credit" && (parseFloat(p.amount) || 0) > 0)?.pay_type ?? "cash"

  return (
    <>
      <Modal
        open={open && !picked && !pendingRefund}
        onOpenChange={onOpenChange}
        title="客户退货"
        description={
          ownOnly
            ? "这里只列今天你自己开的小票。别的小票退货，请找老板或店长。"
            : "输入小票上的单号，或者在最近的销售单里点一张，再选要退的商品。"
        }
        className="max-w-lg"
        footer={
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        }
      >
        {noReturn ? (
          <p className="mt-4 py-4 text-center text-sm text-muted-foreground">退货请找老板或店长</p>
        ) : (
          <div className="mt-4 space-y-3">
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                search(keyword.trim())
              }}
            >
              <Input
                aria-label="小票单号"
                placeholder="输入单号，例如 SL-20261010-0001"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
              />
              <Button type="submit" variant="outline" disabled={loading}>
                查找
              </Button>
            </form>
            {error && <ErrorBanner>{error}</ErrorBanner>}
            {loading && <p className="text-sm text-muted-foreground">正在查找…</p>}
            {!loading && !error && bills.length === 0 && (
              <p className="py-4 text-center text-sm text-muted-foreground">
                {ownOnly ? "今天你还没有开过可以退的小票。别的小票退货，请找老板或店长" : "没有找到已出库的销售单"}
              </p>
            )}
            <ul className="max-h-80 space-y-1 overflow-y-auto">
              {bills.map((b) => (
                <li key={b.id}>
                  <button
                    type="button"
                    className="flex w-full items-center justify-between gap-3 rounded-lg border border-border px-3 py-2 text-left text-sm transition-colors hover:bg-muted/50 disabled:opacity-50"
                    disabled={opening !== null}
                    onClick={() => pick(b)}
                  >
                    <span>
                      <span className="block font-mono text-xs font-medium">{b.bill_no}</span>
                      <span className="block text-xs text-muted-foreground">{formatDateTime(b.bill_date)}</span>
                    </span>
                    <span className="font-mono tabular-nums">
                      {opening === b.id ? "打开中…" : formatCNY(parseFloat(b.total_amount))}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </Modal>

      <RefundRetryModal
        pending={pendingRefund}
        submitting={recording}
        error={refundError}
        onRecord={recordRefund}
        onClose={leaveRefund}
      />

      {picked && (
        <SaleReturnDialog
          open
          onOpenChange={(o) => {
            if (!o) setPicked(null)
          }}
          items={picked.items}
          productNames={productNames}
          submitting={submitting}
          error={returnError}
          refund={{
            total: picked.head.total_amount,
            paid: picked.head.paid_amount,
            returnedAmount: picked.returned_amount ?? "0",
            defaultMethod: refundMethod,
          }}
          onSubmit={submit}
        />
      )}
    </>
  )
}
