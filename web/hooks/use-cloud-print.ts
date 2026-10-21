"use client"

import { useEffect, useState } from "react"
import { getTodoSummary } from "@/lib/api/todo"
import { getPrintSettings } from "@/lib/api/print-settings"
import { useTenantId } from "@/hooks/use-tenant-id"

export interface CloudPrintInfo {
  /** The platform has a cloud-print account. */
  available: boolean
  /** The shop has saved a printer. */
  configured: boolean
  /** Send every checkout to the cloud printer by itself. */
  autoPrint: boolean
}

const OFF: CloudPrintInfo = { available: false, configured: false, autoPrint: false }
const STORE_KEY = "tally-cloud-print"

let memo: CloudPrintInfo | null = null

/** What this device last learned, so the first render after a checkout already knows. */
function remembered(): CloudPrintInfo | null {
  if (memo) return memo
  try {
    const raw = window.localStorage.getItem(STORE_KEY)
    return raw ? { ...OFF, ...(JSON.parse(raw) as Partial<CloudPrintInfo>) } : null
  } catch {
    return null
  }
}

/** 读本机缓存的云打印能力（不触发请求）；补发云打印、预取共用。 */
export function readCloudPrintInfo(): CloudPrintInfo | null {
  return remembered()
}

export function remember(info: CloudPrintInfo) {
  memo = info
  try {
    window.localStorage.setItem(STORE_KEY, JSON.stringify(info))
  } catch {
    // storage unavailable: the memo still serves this page load
  }
}

/** Forget what was learned (the settings page calls this after saving a printer). */
export function forgetCloudPrint() {
  memo = null
  try {
    window.localStorage.removeItem(STORE_KEY)
  } catch {
    // ignore
  }
}

/**
 * useCloudPrint reads, once per mount, whether cloud printing exists on this server
 * (the todo summary's capability flag) and whether the shop has a printer with
 * auto-print on. Any failure counts as "not available": the browser print path stays.
 */
export function useCloudPrint(): CloudPrintInfo & { ready: boolean } {
  const tenantId = useTenantId()
  const [info, setInfo] = useState<CloudPrintInfo | null>(() => (typeof window === "undefined" ? null : remembered()))

  useEffect(() => {
    const ctrl = new AbortController()
    ;(async () => {
      try {
        const s = await getTodoSummary({ tenantId, signal: ctrl.signal })
        if (s?.cloud_print_available !== true) {
          if (!ctrl.signal.aborted) {
            remember(OFF)
            setInfo(OFF)
          }
          return
        }
        const p = (await getPrintSettings(tenantId, ctrl.signal)).printer
        const next: CloudPrintInfo = { available: true, configured: !!p?.sn, autoPrint: !!p?.sn && p.auto_print === true }
        if (!ctrl.signal.aborted) {
          remember(next)
          setInfo(next)
        }
      } catch {
        // keep whatever was remembered; a failed read must never block the till
      }
    })()
    return () => ctrl.abort()
  }, [tenantId])

  return { ...(info ?? OFF), ready: info !== null }
}
