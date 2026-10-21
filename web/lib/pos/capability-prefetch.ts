import { getTodoSummary } from "@/lib/api/todo"
import { EMPTY_PRINT_SETTINGS, getPrintSettings, type PrintSettings } from "@/lib/api/print-settings"
import { remember, type CloudPrintInfo } from "@/hooks/use-cloud-print"

const SETTINGS_KEY = "tally-print-settings-cache"

/** 店名/地址/页脚的本机缓存，离线小票用；读不到就是空。 */
export function readCachedPrintSettings(): PrintSettings {
  try {
    const raw = window.localStorage.getItem(SETTINGS_KEY)
    return raw ? { ...EMPTY_PRINT_SETTINGS, ...(JSON.parse(raw) as Partial<PrintSettings>) } : EMPTY_PRINT_SETTINGS
  } catch {
    return EMPTY_PRINT_SETTINGS
  }
}

function cacheSettings(s: PrintSettings) {
  try {
    // 云打印机密钥（已脱敏）不用于离线小票，不落缓存。
    const { printer: _printer, ...rest } = s
    void _printer
    window.localStorage.setItem(SETTINGS_KEY, JSON.stringify(rest))
  } catch {
    // 存储不可用：忽略
  }
}

/**
 * 收银台挂载时预取云打印能力与打印设置，写进 use-cloud-print 读的同一份本机缓存，
 * 新设备第一单就知道该不该走云打印（避免浏览器和云打印机各出一张）。任何失败都静默。
 */
export async function prefetchCashierCapabilities(tenantId?: string, signal?: AbortSignal): Promise<void> {
  try {
    const s = await getTodoSummary({ tenantId, signal })
    if (signal?.aborted) return
    if (s?.cloud_print_available !== true) {
      remember({ available: false, configured: false, autoPrint: false })
      return
    }
    const settings = await getPrintSettings(tenantId, signal)
    if (signal?.aborted) return
    cacheSettings(settings)
    const p = settings.printer
    const info: CloudPrintInfo = { available: true, configured: !!p?.sn, autoPrint: !!p?.sn && p.auto_print === true }
    remember(info)
  } catch {
    // 离线或接口失败：保留已有缓存
  }
}
