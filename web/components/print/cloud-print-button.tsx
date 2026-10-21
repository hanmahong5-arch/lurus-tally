"use client"

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { useCloudPrint } from "@/hooks/use-cloud-print"
import { useTenantId } from "@/hooks/use-tenant-id"
import { sendCloudReceipt } from "@/lib/pos/cloud-print-send"

/**
 * 云打印按钮：把这张销售单发到店里的云打印机。平台没开通云打印时整个不显示。
 * 一分钟内重复点同一张单会先确认一次。
 */
export function CloudPrintButton({ billId, className, label = "云打印" }: { billId: string; className?: string; label?: string }) {
  const { available } = useCloudPrint()
  const tenantId = useTenantId()
  const [busy, setBusy] = useState(false)
  if (!available) return null
  return (
    <Button
      variant="outline"
      size="sm"
      className={className}
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        try {
          await sendCloudReceipt(billId, { tenantId })
        } finally {
          setBusy(false)
        }
      }}
    >
      {busy ? "发送中…" : label}
    </Button>
  )
}
