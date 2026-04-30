"use client"

import { useState, useEffect, useRef } from "react"
import { draftGet, draftSet, draftDel } from "@/lib/draft/idb-storage"
import { trackEvent } from "@/lib/telemetry"

export type DraftStatus = "local" | "synced" | "none"

export interface UseDraftResult<T> {
  value: T
  setValue: (v: T) => void
  status: DraftStatus
  markSubmitted: () => Promise<void>
  discardDraft: () => Promise<void>
  /** Non-null when a draft was loaded from IDB on mount. */
  restoredAt: Date | null
}

/** Payload shape stored in IDB under each draft key. */
interface DraftRecord<T> {
  value: T
  savedAt: string // ISO timestamp
}

/** 7 days in milliseconds. */
const DRAFT_TTL_MS = 7 * 24 * 60 * 60 * 1000

/** Debounce delay for IDB writes on each setValue call (ms). */
const WRITE_DEBOUNCE_MS = 500

/**
 * useDraft persists form state to IndexedDB so that users can recover work
 * after accidental navigation or tab close.
 *
 * @param key   Namespaced draft key, e.g. "draft:product:new".
 * @param initial  Initial form value used when no draft exists.
 */
export function useDraft<T>(key: string, initial: T): UseDraftResult<T> {
  const [value, setValueState] = useState<T>(initial)
  const [status, setStatus] = useState<DraftStatus>("none")
  const [restoredAt, setRestoredAt] = useState<Date | null>(null)

  // Ref to hold the debounce timer handle.
  const writeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // On mount: attempt to load a draft from IDB.
  useEffect(() => {
    if (typeof window === "undefined") return

    draftGet<DraftRecord<T>>(key)
      .then((record) => {
        if (!record) return

        const age = Date.now() - new Date(record.savedAt).getTime()
        if (age > DRAFT_TTL_MS) {
          // Draft is too old — discard silently.
          draftDel(key).catch(() => undefined)
          return
        }

        setValueState(record.value)
        setStatus("local")
        const at = new Date(record.savedAt)
        setRestoredAt(at)
        trackEvent("draft_restore")
      })
      .catch(() => {
        // IDB unavailable or other error — silently fall back to initial value.
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  // Cleanup timer on unmount.
  useEffect(() => {
    return () => {
      if (writeTimerRef.current !== null) {
        clearTimeout(writeTimerRef.current)
      }
    }
  }, [])

  function setValue(v: T): void {
    setValueState(v)
    setStatus("local")

    // Debounced IDB write.
    if (writeTimerRef.current !== null) {
      clearTimeout(writeTimerRef.current)
    }
    writeTimerRef.current = setTimeout(() => {
      const record: DraftRecord<T> = { value: v, savedAt: new Date().toISOString() }
      draftSet(key, record).catch(() => undefined)
      writeTimerRef.current = null
    }, WRITE_DEBOUNCE_MS)
  }

  async function markSubmitted(): Promise<void> {
    if (writeTimerRef.current !== null) {
      clearTimeout(writeTimerRef.current)
      writeTimerRef.current = null
    }
    setStatus("synced")
    setRestoredAt(null)
    await draftDel(key)
  }

  async function discardDraft(): Promise<void> {
    if (writeTimerRef.current !== null) {
      clearTimeout(writeTimerRef.current)
      writeTimerRef.current = null
    }
    setValueState(initial)
    setStatus("none")
    setRestoredAt(null)
    await draftDel(key)
  }

  return { value, setValue, status, markSubmitted, discardDraft, restoredAt }
}
