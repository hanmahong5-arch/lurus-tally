"use client"

import { Suspense, useState } from "react"
import { useParams, useSearchParams } from "next/navigation"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { getSaleBill, type SaleBillDetail } from "@/lib/api/sale"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useBillPartyNames } from "@/hooks/use-bill-party-names"
import { formatDate } from "@/lib/format"
import { PrintShell, PrintNotice, usePaperChoice, type PaperSize } from "@/components/print/print-shell"
import { DocumentSheet } from "@/components/print/document-sheet"
import { printErrorMessage } from "@/components/print/print-error"
import { joinStamps, owedAmount, remarkText, statusStamp } from "@/components/print/print-format"
import { usePrintSettings, useReadyOrTimeout } from "@/components/print/use-print-data"
import { usePrintProducts, PRODUCT_LOOKUP_WARNING } from "@/components/print/use-print-products"
import { useCustomerContact } from "@/components/print/use-customer-contact"

const OPTIONS: PaperSize[] = ["a5", "a4"]

function SalePrint() {
  const { id } = useParams<{ id: string }>()
  const autoPrint = useSearchParams().get("auto") === "1"
  const tenantId = useTenantId()
  const [detail, setDetail] = useState<SaleBillDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [paper, setPaper] = usePaperChoice("tally-print-paper-sale", OPTIONS, "a5")
  const { settings, loaded } = usePrintSettings(tenantId)

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
  const { contact, loaded: contactLoaded } = useCustomerContact(detail?.head.partner_id, tenantId)
  const names = useBillPartyNames({
    kind: "customer",
    partnerId: detail?.head.partner_id,
    partnerName: detail?.head.partner_name,
    tenantId,
  })
  const namesDone = productIds.every((p) => products[p]) && contactLoaded && (!detail?.head.partner_id || Boolean(names.partnerName))
  const ready = useReadyOrTimeout(Boolean(detail) && loaded && namesDone, Boolean(detail)) && Boolean(detail)

  const subType = detail?.head.sub_type
  const isReturn = subType === "销售退货"
  // This sheet is for sales and sale returns only; a purchase id must not print as a delivery note.
  const wrongType = Boolean(subType) && subType !== "销售" && !isReturn
  const title = isReturn ? "退货单" : "送货单"

  if (wrongType) return <PrintNotice>找不到这张单据，请回到单据详情重新打开。</PrintNotice>

  return (
    <PrintShell title={title} paper={paper} options={OPTIONS} onPaper={setPaper} autoPrint={autoPrint} ready={ready} notice={productsFailed || (ready && productIds.some((p) => !products[p])) ? PRODUCT_LOOKUP_WARNING : undefined}>
      {error && <PrintNotice>{error}</PrintNotice>}
      {!error && !detail && <PrintNotice>正在准备单据…</PrintNotice>}
      {detail && (
        <DocumentSheet
          shop={settings}
          title={title}
          stamp={joinStamps(isReturn ? "退货" : undefined, statusStamp(detail.head.status))}
          billNo={detail.head.bill_no}
          dateText={formatDate(detail.head.bill_date)}
          meta={[
            ["客户", names.partnerName || (detail.head.partner_id ? "" : "散客")],
            ...(contact.contact ? [["联系人", contact.contact] as [string, string]] : []),
            ...(contact.phone ? [["电话", contact.phone] as [string, string]] : []),
          ]}
          metaWide={[["收货地址", contact.address]]}
          paper={paper === "a4" ? "a4" : "a5"}
          lines={detail.items.map((it) => ({
            name: products[it.product_id]?.name || "商品",
            spec: products[it.product_id]?.spec,
            unit: it.unit_name,
            qty: it.qty,
            price: it.unit_price,
            amount: it.line_amount,
          }))}
          totalAmount={detail.head.total_amount}
          settle={
            isReturn
              ? undefined
              : [
                  ...(Number(detail.returned_amount) > 0 ? [["已退货", detail.returned_amount ?? ""] as [string, string]] : []),
                  ["已收款", detail.head.paid_amount],
                  ["未收款", owedAmount(detail.receivable_amount ?? detail.head.receivable_amount)],
                ]
          }
          remark={remarkText(detail.head.remark)}
          signLabels={isReturn ? ["退货人", "收货人", "日期"] : ["送货人", "收货人签字", "日期"]}
        />
      )}
    </PrintShell>
  )
}

export default function SalePrintPage() {
  return (
    <Suspense fallback={null}>
      <SalePrint />
    </Suspense>
  )
}
