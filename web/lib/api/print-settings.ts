/**
 * API wrapper for the shop's print header / receipt footer.
 * Backend: GET/PUT /api/v1/tenant/print-settings (stored in tenant.settings).
 */
import { apiFetch } from "./client"

export interface PrintSettings {
  shop_name: string
  phone: string
  address: string
  receipt_footer: string
  /** The shop's cloud printer; absent on shops that never set one up. */
  printer?: CloudPrinterSettings
}

/** Cloud printer block. `key` comes back masked (last 4 only); send the masked value back to keep it. */
export interface CloudPrinterSettings {
  sn: string
  key: string
  name: string
  paper: "58" | "80"
  auto_print: boolean
}

export const EMPTY_PRINT_SETTINGS: PrintSettings = {
  shop_name: "",
  phone: "",
  address: "",
  receipt_footer: "",
}

export async function getPrintSettings(tenantId?: string, signal?: AbortSignal): Promise<PrintSettings> {
  const res = await apiFetch<Partial<PrintSettings>>("/tenant/print-settings", { tenantId, signal })
  return { ...EMPTY_PRINT_SETTINGS, ...res }
}

export async function putPrintSettings(body: PrintSettings, tenantId?: string): Promise<PrintSettings> {
  const res = await apiFetch<Partial<PrintSettings>>("/tenant/print-settings", {
    method: "PUT",
    body: JSON.stringify(body),
    tenantId,
  })
  return { ...EMPTY_PRINT_SETTINGS, ...res }
}
