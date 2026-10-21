/**
 * API wrapper for cloud receipt printing (飞鹅云打印机).
 * Backend: POST /print/receipts, GET /tenant/printer/status, POST /tenant/printer/test.
 * Error codes: printer_not_configured (409), printer_offline (503), cloud_print_unavailable (501).
 */
import { apiFetch } from "./client"

export interface PrinterStatus {
  /** The shop has saved a printer. */
  configured: boolean
  /** The device is online and ready. */
  online: boolean
  /** The vendor's status sentence, or a "state unknown" note. */
  status: string
  /** The platform has a cloud-print account; false hides every cloud-print control. */
  cloud_print_available: boolean
}

/** Send the sale `billId` to the shop's cloud printer; resolves with the vendor order id. */
export async function printReceipt(
  billId: string,
  opts: { times?: number; tenantId?: string } = {},
): Promise<string> {
  const res = await apiFetch<{ order_id: string }>("/print/receipts", {
    method: "POST",
    body: JSON.stringify({ bill_id: billId, times: opts.times ?? 1 }),
    tenantId: opts.tenantId,
    silent: true,
  })
  return res.order_id
}

export function getPrinterStatus(opts: { tenantId?: string; signal?: AbortSignal } = {}): Promise<PrinterStatus> {
  return apiFetch<PrinterStatus>("/tenant/printer/status", { tenantId: opts.tenantId, signal: opts.signal, silent: true })
}

/** Print a short test page. */
export async function printTestPage(tenantId?: string): Promise<string> {
  const res = await apiFetch<{ order_id: string }>("/tenant/printer/test", {
    method: "POST",
    body: JSON.stringify({}),
    tenantId,
    silent: true,
  })
  return res.order_id
}
