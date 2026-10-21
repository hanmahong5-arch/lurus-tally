"use client"

import { useEffect, useRef, useState, type ReactNode } from "react"
import Link from "next/link"
import { useSignOutBroadcast } from "@/hooks/use-signout-broadcast"

export type PaperSize = "a4" | "a5" | "58" | "80"

export const PAPER_LABEL: Record<PaperSize, string> = {
  a4: "A4 纸",
  a5: "A5 纸",
  "58": "58 毫米小票纸",
  "80": "80 毫米小票纸",
}

const PAGE_WIDTH_MM: Record<PaperSize, number> = { a4: 210, a5: 148, "58": 58, "80": 80 }

/** `@page size` has no "auto" height for a custom width (browsers drop the whole rule), so a till paper gets a measured height. */
const MIN_RECEIPT_MM = 40
const RECEIPT_TAIL_MM = 4
const PX_PER_MM = 96 / 25.4

export function pageSizeRule(paper: PaperSize, receiptHeightMm: number): string {
  if (paper === "a4") return "A4"
  if (paper === "a5") return "A5"
  return `${PAGE_WIDTH_MM[paper]}mm ${receiptHeightMm}mm`
}

/** Message the print page posts to its parent frame once the print dialog has closed. */
export const PRINT_DONE_MESSAGE = "tally:print-done"
/** Message the hidden print frame posts instead when it cannot print a complete document (it has no toolbar to say so). */
export const PRINT_INCOMPLETE_MESSAGE = "tally:print-incomplete"

/** Remembers the paper choice per document kind on this device. */
export function usePaperChoice(storageKey: string, options: PaperSize[], fallback: PaperSize): [PaperSize, (p: PaperSize) => void] {
  const [paper, setPaper] = useState<PaperSize>(fallback)
  useEffect(() => {
    try {
      const saved = window.localStorage.getItem(storageKey) as PaperSize | null
      if (saved && options.includes(saved)) setPaper(saved)
    } catch {
      // storage unavailable (private mode): keep the default
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey])
  function choose(p: PaperSize) {
    setPaper(p)
    try {
      window.localStorage.setItem(storageKey, p)
    } catch {
      // ignore
    }
  }
  return [paper, choose]
}

interface PrintShellProps {
  title: string
  paper: PaperSize
  options: PaperSize[]
  onPaper: (p: PaperSize) => void
  /** Open the print dialog as soon as the document is ready (used by the POS hidden frame). */
  autoPrint: boolean
  ready: boolean
  /** Screen-only warning shown in the toolbar (e.g. some product details did not load). */
  notice?: string
  children: ReactNode
}

/**
 * PrintShell is the chrome shared by every print page: a screen-only toolbar
 * (paper choice, print button) above a white "sheet". Under @media print the
 * toolbar disappears and the sheet fills the page; @page margin 0 also keeps
 * the browser from stamping its own header / URL on the paper.
 */
export function PrintShell({ title, paper, options, onPaper, autoPrint, ready, notice, children }: PrintShellProps) {
  useSignOutBroadcast()
  const thermal = paper === "58" || paper === "80"
  const [embedded, setEmbedded] = useState(false)
  useEffect(() => setEmbedded(window.parent !== window), [])
  const sheetRef = useRef<HTMLDivElement>(null)
  const [heightMm, setHeightMm] = useState(MIN_RECEIPT_MM)

  // Track the sheet's rendered height so the till paper is cut to the content.
  useEffect(() => {
    const el = sheetRef.current
    if (!thermal || !el) return
    const measure = () => setHeightMm(Math.max(MIN_RECEIPT_MM, Math.ceil(el.offsetHeight / PX_PER_MM) + RECEIPT_TAIL_MM))
    measure()
    if (typeof ResizeObserver === "undefined") return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [thermal, paper, ready])

  useEffect(() => {
    document.title = title
  }, [title])

  useEffect(() => {
    if (!autoPrint || !ready) return
    const t = setTimeout(() => {
      // The hidden frame shows no toolbar, so an incomplete document is reported to the till instead of printed.
      if (window.parent !== window && notice) {
        window.parent.postMessage(PRINT_INCOMPLETE_MESSAGE, window.location.origin)
        return
      }
      window.print()
      if (window.parent !== window) window.parent.postMessage(PRINT_DONE_MESSAGE, window.location.origin)
    }, 300)
    return () => clearTimeout(t)
  }, [autoPrint, ready, notice])

  return (
    <div className="min-h-screen bg-neutral-200 text-black print:min-h-0 print:bg-white">
      <style>{`@page { size: ${pageSizeRule(paper, heightMm)}; margin: 0 } @media print { html, body { background: #fff !important } .print-sheet { margin: 0 !important; box-shadow: none !important } }`}</style>
      {!embedded && (
        <div className="flex flex-wrap items-center gap-3 border-b border-neutral-300 bg-white px-4 py-3 text-sm print:hidden">
          <span className="font-medium">{title}</span>
          <label className="flex items-center gap-1.5">
            纸张
            <select
              className="h-8 rounded border border-neutral-300 bg-white px-2"
              value={paper}
              onChange={(e) => onPaper(e.target.value as PaperSize)}
            >
              {options.map((o) => (
                <option key={o} value={o}>
                  {PAPER_LABEL[o]}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            disabled={!ready}
            onClick={() => window.print()}
            className="h-8 rounded bg-black px-4 text-white disabled:opacity-50"
          >
            打印
          </button>
          {notice && (
            <span role="alert" className="text-red-600">
              {notice}
            </span>
          )}
          <Link href="/settings/print" className="text-neutral-600 underline" target="_blank">
            设置店名与小票页脚
          </Link>
        </div>
      )}
      <div
        ref={sheetRef}
        className={thermal ? "print-sheet mx-auto my-4 bg-white" : "print-sheet mx-auto my-4 bg-white shadow"}
        style={{ width: `${PAGE_WIDTH_MM[paper]}mm` }}
      >
        {children}
      </div>
    </div>
  )
}

export function PrintNotice({ children }: { children: ReactNode }) {
  return <div className="p-8 text-center text-sm text-neutral-700">{children}</div>
}
