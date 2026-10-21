import { toast } from "sonner"
import { printReceipt } from "@/lib/api/print"
import { extractApiError } from "@/lib/api/errors"

/** A second send of the same bill inside this window asks the cashier first. */
export const REPRINT_GUARD_MS = 60_000

const lastSent = new Map<string, number>()

/** True when `billId` went to the cloud printer less than REPRINT_GUARD_MS ago. */
export function printedRecently(billId: string, now: number = Date.now()): boolean {
  const at = lastSent.get(billId)
  return at !== undefined && now - at < REPRINT_GUARD_MS
}

/** Test hook: forget every send. */
export function resetCloudPrintGuard() {
  lastSent.clear()
}

/**
 * sendCloudReceipt sends the receipt to the cloud printer and tells the cashier
 * how it went. A repeat inside 60 s needs a confirm (the caller's `force` skips
 * it for the automatic first send). Resolves true when the printer took it.
 */
export async function sendCloudReceipt(
  billId: string,
  opts: { tenantId?: string; force?: boolean } = {},
): Promise<boolean> {
  if (!opts.force && printedRecently(billId) && !window.confirm("刚刚已经打印过这张小票了，确定再打印一张吗？")) {
    return false
  }
  try {
    await printReceipt(billId, { tenantId: opts.tenantId })
    lastSent.set(billId, Date.now())
    toast.success("已发送到云打印机")
    return true
  } catch (e) {
    toast.error(`云打印没成功：${extractApiError(e).message}`)
    return false
  }
}
