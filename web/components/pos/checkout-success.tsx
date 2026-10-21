"use client"

import { useEffect, useRef, useState } from "react"
import Decimal from "decimal.js"
import { PRINT_DONE_MESSAGE, PRINT_INCOMPLETE_MESSAGE } from "@/components/print/print-shell"
import { saveTender } from "@/components/print/tender"
import { HideInCashierMode } from "@/components/cashier-mode-gate"
import { useRole } from "@/hooks/use-role"
import { getAutoPrintReceipt, getReceiptPaper } from "@/lib/pos/print-prefs"
import { ReceiptSheet } from "@/components/print/receipt-sheet"
import { readCachedPrintSettings } from "@/lib/pos/capability-prefetch"
import type { QueueEntry } from "@/lib/pos/checkout-queue"
import { useCloudPrint } from "@/hooks/use-cloud-print"
import { useTenantId } from "@/hooks/use-tenant-id"
import { sendCloudReceipt } from "@/lib/pos/cloud-print-send"

interface CheckoutSuccessProps {
  billNo: string
  totalAmount: string
  /** Sale bill id; when given the cashier can print the till receipt from here. */
  billId?: string
  /** Cash handed over and change given back, kept so the receipt can show (and later reprint) them. */
  receivedAmount?: string
  changeAmount?: string
  /** Still owed by the customer after this sale (a credit sale); string decimal. */
  owedAmount?: string
  /** 离线入队的单号（OFF-xxxx-n）；有值时显示「离线单号」，联网后才会换成正式单号。 */
  offlineRef?: string
  /** 离线入队的台账条目；有值时用它渲染离线小票，走浏览器打印（不调云打印）。 */
  offlineEntry?: QueueEntry
  onDismiss: () => void
}

/** Without a receipt to print the overlay is a 1 s flash; with one it waits long enough to press Enter. */
const FLASH_MS = 1000
const PRINT_WAIT_MS = 8000
/** 离线单要让收银员看清单号与提示。 */
const OFFLINE_WAIT_MS = 3000
/** Once a print is under way the frame reports back; this is only the backstop if it never does. */
const PRINT_BACKSTOP_MS = 30000
/** A barcode scanner ends each scan with Enter; right after checkout that Enter must not print again. */
const KEY_GUARD_MS = 2000

/**
 * CheckoutSuccess displays a full-screen overlay confirming a successful checkout.
 * Auto-dismisses (1 s, or 8 s when a receipt can be printed). With a bill id,
 * Enter prints the receipt (only while the print button has focus, and not in the
 * first moments, so a scanner's trailing Enter never reprints) and Esc closes; the receipt prints from a hidden
 * frame so the till page never navigates away.
 */
export function CheckoutSuccess({ billNo, totalAmount, billId, receivedAmount, changeAmount, owedAmount, offlineRef, offlineEntry, onDismiss }: CheckoutSuccessProps) {
  const { known, can } = useRole()
  const canEditShop = !known || can("settings.manage")
  // Bumped per print request so each press mounts a fresh frame (and prints again).
  const [printRun, setPrintRun] = useState(0)
  // The frame found the receipt incomplete and did not print it.
  const [printIncomplete, setPrintIncomplete] = useState(false)

  const printBtnRef = useRef<HTMLButtonElement>(null)
  const shownAt = useRef(Date.now())
  const scanned = useRef(false)

  const printing = printRun > 0
  const owed = new Decimal(owedAmount || "0")

  // Saved before the first print is requested (effects run in order), so the receipt sees it.
  useEffect(() => {
    if (billId && receivedAmount !== undefined) saveTender(billId, { received: receivedAmount, change: changeAmount ?? "0" })
  }, [billId, receivedAmount, changeAmount])

  useEffect(() => {
    const wait = !billId ? (offlineEntry ? PRINT_WAIT_MS : offlineRef ? OFFLINE_WAIT_MS : FLASH_MS) : printing && !printIncomplete ? PRINT_BACKSTOP_MS : PRINT_WAIT_MS
    const timer = setTimeout(onDismiss, wait)
    return () => clearTimeout(timer)
  }, [onDismiss, billId, offlineRef, offlineEntry, printing, printIncomplete])

  // 云打印机开了自动打印时，结账后直接发到云打印机；发不出去就回落到下面的浏览器打印。
  const cloud = useCloudPrint()
  const tenantId = useTenantId()
  const cloudAutoSent = useRef(false)
  const browserAutoRan = useRef(false)

  useEffect(() => {
    if (!billId) return
    if (cloud.autoPrint && !browserAutoRan.current) {
      if (cloudAutoSent.current) return
      cloudAutoSent.current = true
      void sendCloudReceipt(billId, { tenantId, force: true }).then((ok) => {
        if (!ok) setPrintRun((n) => (n === 0 ? 1 : n))
      })
      return
    }
    if (getAutoPrintReceipt() && !cloudAutoSent.current) {
      browserAutoRan.current = true
      setPrintRun(1)
    }
  }, [billId, cloud.autoPrint, tenantId])

  // 离线单：小票直接在本页渲染，用浏览器打印；店铺开了自动打印时进来就打一次。
  const offlineAutoRan = useRef(false)
  const printOffline = () => window.print()
  useEffect(() => {
    if (!offlineEntry || offlineAutoRan.current) return
    offlineAutoRan.current = true
    if (getAutoPrintReceipt()) setTimeout(() => window.print(), 100)
  }, [offlineEntry])

  useEffect(() => {
    if (!billId) return
    function onKey(e: KeyboardEvent) {
      if (e.key.length === 1 && e.key !== " " && !e.ctrlKey && !e.metaKey && !e.altKey) {
        // A printable key now is the next customer's scan (or typing), not a print request: hand the till back.
        scanned.current = true
        onDismiss()
      } else if (e.key === "Enter") {
        // Anywhere but the print button, Enter belongs to whatever else has focus.
        if (document.activeElement !== printBtnRef.current) return
        e.preventDefault()
        if (scanned.current || Date.now() - shownAt.current < KEY_GUARD_MS) return
        setPrintIncomplete(false)
        setPrintRun((n) => n + 1)
      } else if (e.key === "Escape") {
        onDismiss()
      }
    }
    function onMessage(e: MessageEvent) {
      if (e.origin !== window.location.origin) return
      if (e.data === PRINT_DONE_MESSAGE) onDismiss()
      else if (e.data === PRINT_INCOMPLETE_MESSAGE) setPrintIncomplete(true)
    }
    window.addEventListener("keydown", onKey)
    window.addEventListener("message", onMessage)
    return () => {
      window.removeEventListener("keydown", onKey)
      window.removeEventListener("message", onMessage)
    }
  }, [billId, onDismiss])

  return (
    <div className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-emerald-500">
      {/* Check icon */}
      <div className="mb-6">
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="white"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="h-24 w-24"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="10" />
          <path d="M9 12l2 2 4-4" />
        </svg>
      </div>

      {/* Amount */}
      <div className="mb-2 text-5xl font-bold tabular-nums text-white">
        ¥{totalAmount}
      </div>
      <div className="mb-4 text-xl text-emerald-100">
        {owed.gt(0) ? `已记赊账，客户欠 ¥${owed.toFixed(2)}` : "已收款"}
      </div>

      {/* Bill number */}
      {offlineRef ? (
        <div className="mb-8 text-center text-sm text-emerald-100">
          <div className="font-mono text-emerald-200">离线单号 {offlineRef}</div>
          <div>已先记在本机，联网后自动上传</div>
        </div>
      ) : (
        <div className="mb-8 font-mono text-sm text-emerald-200">{billNo}</div>
      )}

      {printIncomplete && (
        <div role="alert" className="mb-4 rounded-lg bg-white/90 px-4 py-2 text-sm text-red-700">
          小票没打全（有商品信息没加载出来），请到「今日结单」里补打
        </div>
      )}

      <div className="flex items-center gap-3">
        {billId && (
          <button
            ref={printBtnRef}
            autoFocus
            onClick={() => {
              setPrintIncomplete(false)
              setPrintRun((n) => n + 1)
            }}
            className="rounded-lg bg-white px-6 py-2 text-sm font-medium text-emerald-700 hover:bg-emerald-50 transition-colors"
            aria-label="打印小票"
          >
            打印小票（回车）
          </button>
        )}
        {offlineEntry && (
          <button
            onClick={printOffline}
            className="rounded-lg bg-white px-6 py-2 text-sm font-medium text-emerald-700 hover:bg-emerald-50 transition-colors"
            aria-label="打印离线小票"
          >
            打印离线小票
          </button>
        )}
        {billId && cloud.available && cloud.configured && (
          <button
            onClick={() => void sendCloudReceipt(billId, { tenantId })}
            className="rounded-lg bg-white px-6 py-2 text-sm font-medium text-emerald-700 hover:bg-emerald-50 transition-colors"
            aria-label="云打印重打"
          >
            重打
          </button>
        )}
        <button
          onClick={onDismiss}
          className="rounded-lg border border-white/50 px-6 py-2 text-sm font-medium text-white hover:bg-white/20 transition-colors"
          aria-label="关闭成功提示"
        >
          关闭
        </button>
      </div>

      {billId && (
        // Back-office page: a cashier-mode counter must not reach it.
        <HideInCashierMode>
          <a href="/settings/print" target="_blank" rel="noreferrer" className="mt-4 text-xs text-emerald-100 underline">
            {canEditShop ? "设置店名、小票纸宽和自动打印" : "设置小票纸宽和自动打印"}
          </a>
        </HideInCashierMode>
      )}

      {offlineEntry && <OfflineReceiptPrint entry={offlineEntry} />}

      {billId && printRun > 0 && (
        <iframe
          key={printRun}
          title="小票打印"
          src={`/print/pos/${billId}?auto=1`}
          className="pointer-events-none fixed h-px w-px border-0 opacity-0"
          aria-hidden="true"
          tabIndex={-1}
        />
      )}
    </div>
  )
}

/** 只在打印时可见的离线小票：屏幕上隐藏，window.print() 时其余内容全部隐去。 */
function OfflineReceiptPrint({ entry }: { entry: QueueEntry }) {
  const wide = getReceiptPaper() === "80"
  return (
    <>
      <style>{`
        .offline-receipt-print { display: none; }
        @media print {
          @page { margin: 0; }
          body * { visibility: hidden !important; }
          .offline-receipt-print, .offline-receipt-print * { visibility: visible !important; }
          .offline-receipt-print { display: block; position: absolute; left: 0; top: 0; width: ${wide ? "80mm" : "58mm"}; background: #fff; }
        }
      `}</style>
      <div className="offline-receipt-print" data-testid="offline-receipt-print">
        <ReceiptSheet kind="offline" entry={entry} shop={readCachedPrintSettings()} wide={wide} />
      </div>
    </>
  )
}
