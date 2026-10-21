"use client"

import { Suspense, useState } from "react"
import { useSearchParams } from "next/navigation"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { getStatement, type ReceiptDirection, type Statement } from "@/lib/api/receipts"
import { extractApiError } from "@/lib/api/errors"
import { PrintShell, PrintNotice, usePaperChoice, type PaperSize } from "@/components/print/print-shell"
import { usePrintSettings, useReadyOrTimeout } from "@/components/print/use-print-data"
import { StatementTable } from "@/components/statement-table"
import { formatDate } from "@/lib/format"

const OPTIONS: PaperSize[] = ["a4", "a5"]

function errorText(e: unknown): string {
  const { code } = extractApiError(e)
  if (code === "offline" || code === "network") return "网络不通，请检查网络后重新打开。"
  return "对账单暂时打不开，请回到客户或供应商页面重新打开。"
}

function StatementPrint() {
  const sp = useSearchParams()
  const partnerId = sp.get("partner") ?? ""
  const direction: ReceiptDirection = sp.get("direction") === "ap" ? "ap" : "ar"
  const from = sp.get("from") ?? undefined
  const to = sp.get("to") ?? undefined
  const tenantId = useTenantId()
  const [st, setSt] = useState<Statement | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [paper, setPaper] = usePaperChoice("tally-print-paper-statement", OPTIONS, "a4")
  const { settings, loaded } = usePrintSettings(tenantId)

  useAbortableEffect(
    (signal, isCancelled) => {
      if (!partnerId) return
      getStatement(partnerId, { direction, from, to }, { tenantId, signal }).then(
        (r) => !isCancelled() && setSt(r),
        (e) => !isCancelled() && !signal.aborted && setError(errorText(e))
      )
    },
    [partnerId, direction, from, to, tenantId]
  )

  const ready = useReadyOrTimeout(Boolean(st) && loaded, Boolean(st)) && Boolean(st)

  return (
    <PrintShell title="对账单" paper={paper} options={OPTIONS} onPaper={setPaper} autoPrint={sp.get("auto") === "1"} ready={ready}>
      {error && <PrintNotice>{error}</PrintNotice>}
      {!error && !st && <PrintNotice>正在准备对账单…</PrintNotice>}
      {st && (
        <div className="p-[10mm] text-[12px] leading-snug text-black">
          <div className="text-center">
            {settings.shop_name && <div className="text-[14px] font-medium">{settings.shop_name}</div>}
            <h1 className="text-[20px] font-bold tracking-widest">对账单</h1>
            <div className="mt-1">
              {st.direction === "ar" ? "客户" : "供应商"}：{st.partner_name}　{formatDate(st.from)} 至 {formatDate(st.to)}
            </div>
          </div>
          <div className="mt-3">
            <StatementTable st={st} cellClass="border border-black px-1.5 py-1" />
          </div>
          <div className="mt-8 flex justify-between">
            <span>我方签字：</span>
            <span>对方签字：</span>
          </div>
        </div>
      )}
    </PrintShell>
  )
}

export default function StatementPrintPage() {
  return (
    <Suspense fallback={null}>
      <StatementPrint />
    </Suspense>
  )
}
