"use client"

import { Suspense, useEffect, useState } from "react"
import { useParams, useSearchParams } from "next/navigation"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { getSaleBill, type SaleBillDetail } from "@/lib/api/sale"
import { useTenantId } from "@/hooks/use-tenant-id"
import { formatDateTime } from "@/lib/format"
import { PrintShell, PrintNotice, usePaperChoice, type PaperSize } from "@/components/print/print-shell"
import { ReceiptSheet } from "@/components/print/receipt-sheet"
import { printErrorMessage } from "@/components/print/print-error"
import { joinStamps, payTypeText, remarkText, owedAmount, statusStamp } from "@/components/print/print-format"
import { loadTender, tenderFromPayments, type Tender } from "@/components/print/tender"
import { usePrintSettings, useReadyOrTimeout } from "@/components/print/use-print-data"
import { usePrintProducts, PRODUCT_LOOKUP_WARNING } from "@/components/print/use-print-products"

const OPTIONS: PaperSize[] = ["58", "80"]

function PosPrint() {
  const { id } = useParams<{ id: string }>()
  const autoPrint = useSearchParams().get("auto") === "1"
  const tenantId = useTenantId()
  const [detail, setDetail] = useState<SaleBillDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [paper, setPaper] = usePaperChoice("tally-print-paper-pos", OPTIONS, "80")
  const { settings, loaded } = usePrintSettings(tenantId)
  const [localTender, setLocalTender] = useState<Tender | null>(null)
  useEffect(() => setLocalTender(loadTender(id)), [id])

  useAbortableEffect(
    (signal, isCancelled) => {
      getSaleBill(id, tenantId, signal).then(
        (d) => !isCancelled() && setDetail(d),
        (e) => !isCancelled() && !signal.aborted && setError(printErrorMessage(e))
      )
    },
    [id, tenantId]
  )

  const productIds = detail?.items.map((it) => it.product_id) ?? []
  const { products, failed: productsFailed } = usePrintProducts(productIds, tenantId)
  const ready = useReadyOrTimeout(Boolean(detail) && loaded && productIds.every((p) => products[p]), Boolean(detail)) && Boolean(detail)
  const isReturn = detail?.head.sub_type === "销售退货"
  // This receipt is for sales and sale returns only; a purchase id must not print as a till receipt.
  const subType = detail?.head.sub_type
  const wrongType = Boolean(subType) && subType !== "销售" && !isReturn
  // The payment remark is the record of truth (any device); this device's own copy is the fallback.
  const tender = tenderFromPayments(detail?.payments) ?? localTender
  const receivable = detail ? (detail.receivable_amount ?? detail.head.receivable_amount) : undefined

  if (wrongType) return <PrintNotice>找不到这张单据，请回到单据详情重新打开。</PrintNotice>

  return (
    <PrintShell title={isReturn ? "退货小票" : "销售小票"} paper={paper} options={OPTIONS} onPaper={setPaper} autoPrint={autoPrint} ready={ready} notice={productsFailed || (ready && productIds.some((p) => !products[p])) ? PRODUCT_LOOKUP_WARNING : undefined}>
      {error && <PrintNotice>{error}</PrintNotice>}
      {!error && !detail && <PrintNotice>正在准备小票…</PrintNotice>}
      {detail && (
        <ReceiptSheet
          shop={settings}
          billNo={detail.head.bill_no}
          dateText={formatDateTime(detail.head.created_at)}
          lines={detail.items.map((it) => ({
            name: products[it.product_id]?.name || "商品",
            unit: it.unit_name,
            qty: it.qty,
            price: it.unit_price,
            amount: it.line_amount,
          }))}
          totalAmount={detail.head.total_amount}
          paidAmount={isReturn ? undefined : detail.head.paid_amount}
          payMethod={isReturn ? undefined : payTypeText((detail.payments ?? []).map((p) => p.pay_type)) || (owedAmount(receivable) !== "0" ? "赊账" : undefined)}
          receivedAmount={isReturn ? undefined : tender?.received}
          changeAmount={isReturn ? undefined : tender?.change}
          unpaidAmount={isReturn ? undefined : owedAmount(receivable)}
          wide={paper === "80"}
          stamp={joinStamps(isReturn ? "退货" : undefined, statusStamp(detail.head.status))}
          title={isReturn ? "退货小票" : "销售小票"}
          remark={remarkText(detail.head.remark)}
        />
      )}
    </PrintShell>
  )
}

export default function PosPrintPage() {
  return (
    <Suspense fallback={null}>
      <PosPrint />
    </Suspense>
  )
}
