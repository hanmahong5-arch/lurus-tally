"use client"

import { Suspense, useEffect, useState } from "react"
import { useParams, useRouter, useSearchParams } from "next/navigation"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { getPurchaseBill, type BillDetail } from "@/lib/api/purchase"
import { useTenantId } from "@/hooks/use-tenant-id"
import { useBillPartyNames } from "@/hooks/use-bill-party-names"
import { formatDate } from "@/lib/format"
import { PrintShell, PrintNotice, usePaperChoice, type PaperSize } from "@/components/print/print-shell"
import { DocumentSheet } from "@/components/print/document-sheet"
import { printErrorMessage } from "@/components/print/print-error"
import { joinStamps, remarkText, statusStamp } from "@/components/print/print-format"
import { usePrintSettings, useReadyOrTimeout } from "@/components/print/use-print-data"
import { usePrintProducts, PRODUCT_LOOKUP_WARNING } from "@/components/print/use-print-products"
import { useSupplierContact } from "@/components/print/use-supplier-contact"

const OPTIONS: PaperSize[] = ["a5", "a4"]

function PurchasePrint() {
  const { id } = useParams<{ id: string }>()
  const autoPrint = useSearchParams().get("auto") === "1"
  const router = useRouter()
  const tenantId = useTenantId()
  const [detail, setDetail] = useState<BillDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [paper, setPaper] = usePaperChoice("tally-print-paper-purchase", OPTIONS, "a5")
  const { settings, loaded } = usePrintSettings(tenantId)

  useAbortableEffect(
    (signal, isCancelled) => {
      getPurchaseBill(id, tenantId, signal).then(
        (d) => !isCancelled() && setDetail(d),
        (e) => !isCancelled() && !signal.aborted && setError(printErrorMessage(e))
      )
    },
    [id, tenantId]
  )

  const productIds = detail?.items.map((it) => it.product_id) ?? []
  const { products, failed: productsFailed } = usePrintProducts(productIds, tenantId)
  const { contact, loaded: contactLoaded } = useSupplierContact(detail?.head.partner_id, tenantId)
  const names = useBillPartyNames({
    kind: "supplier",
    partnerId: detail?.head.partner_id,
    partnerName: detail?.head.partner_name,
    warehouseId: detail?.head.warehouse_id,
    warehouseName: detail?.head.warehouse_name,
    tenantId,
  })
  const namesDone =
    contactLoaded &&
    productIds.every((p) => products[p]) &&
    (!detail?.head.partner_id || Boolean(names.partnerName)) &&
    (!detail?.head.warehouse_id || Boolean(names.warehouseName))
  const ready = useReadyOrTimeout(Boolean(detail) && loaded && namesDone, Boolean(detail)) && Boolean(detail)

  const subType = detail?.head.sub_type
  const isSaleReturn = subType === "销售退货"
  // Only purchases and purchase returns are printed here; a sale return goes to its own sheet.
  const wrongType = Boolean(subType) && subType !== "采购" && subType !== "采购退货" && !isSaleReturn
  const isReturn = subType === "采购退货"
  const title = isReturn ? "采购退货单" : "采购入库单"

  useEffect(() => {
    if (isSaleReturn) router.replace(`/print/sale/${id}`)
  }, [isSaleReturn, router, id])

  if (isSaleReturn) return <PrintNotice>正在打开退货单…</PrintNotice>
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
            ["供应商", names.partnerName],
            ...(contact.contact ? [["联系人", contact.contact] as [string, string]] : []),
            ...(contact.phone ? [["电话", contact.phone] as [string, string]] : []),
            [isReturn ? "出库仓库" : "入库仓库", names.warehouseName],
          ]}
          metaWide={[["地址", contact.address]]}
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
          remark={remarkText(detail.head.remark)}
          signLabels={isReturn ? ["发货人", "经办人", "日期"] : ["送货人", "验收人", "日期"]}
        />
      )}
    </PrintShell>
  )
}

export default function PurchasePrintPage() {
  return (
    <Suspense fallback={null}>
      <PurchasePrint />
    </Suspense>
  )
}
