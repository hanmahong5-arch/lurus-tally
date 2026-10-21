"use client"

import { useEffect, useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { EMPTY_PRINT_SETTINGS, getPrintSettings, type PrintSettings } from "@/lib/api/print-settings"

/** The shop header / footer; a failed load prints without it rather than blocking the print. */
export function usePrintSettings(tenantId?: string): { settings: PrintSettings; loaded: boolean } {
  const [settings, setSettings] = useState<PrintSettings>(EMPTY_PRINT_SETTINGS)
  const [loaded, setLoaded] = useState(false)
  useAbortableEffect(
    (signal, isCancelled) => {
      getPrintSettings(tenantId, signal).then(
        (s) => {
          if (isCancelled()) return
          setSettings(s)
          setLoaded(true)
        },
        () => {
          if (isCancelled() || signal.aborted) return
          setLoaded(true)
        }
      )
    },
    [tenantId]
  )
  return { settings, loaded }
}

/**
 * True once `done` holds, or `ms` after the document has loaded (`started`) regardless, so a slow
 * name lookup never stalls printing. The clock starts at `started`, not at mount: a slow bill
 * request must not use up the grace period meant for the name lookups.
 */
export function useReadyOrTimeout(done: boolean, started = true, ms = 4000): boolean {
  const [timedOut, setTimedOut] = useState(false)
  useEffect(() => {
    if (!started) return
    const t = setTimeout(() => setTimedOut(true), ms)
    return () => clearTimeout(t)
  }, [started, ms])
  return done || timedOut
}
