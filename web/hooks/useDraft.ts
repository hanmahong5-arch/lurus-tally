"use client"

import { useState, useEffect, useRef } from "react"
import { useSession } from "next-auth/react"
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
 * Resolve the draft owner scope (tenant + user) from the session.
 * Returns "pending" while the session is loading, "" when there is no session
 * (unscoped, legacy key), otherwise "<tenant>:<user>".
 */
function useDraftScope(): string {
  // Every caller sits inside the root <SessionProvider> (app/layout.tsx).
  const session = useSession()
  if (session.status === "loading") return "pending"
  const user = session.data?.user
  if (!user) return ""
  const tenant = user.tenantId ?? ""
  const id = user.id ?? ""
  if (!tenant && !id) return ""
  return `${tenant}:${id}`
}

/** Build the storage key; the scope isolates drafts per tenant and user. */
export function scopedDraftKey(key: string, scope: string): string {
  return scope ? `${key}@${scope}` : key
}

/**
 * useDraft persists form state to IndexedDB so that users can recover work
 * after accidental navigation or tab close.
 *
 * Keys are scoped by tenant + user so switching accounts in one browser never
 * shows another account's draft. A draft under the old unscoped key is
 * migrated to the scoped key once, then removed.
 *
 * @param key   Namespaced draft key, e.g. "draft:product:new".
 * @param initial  Initial form value used when no draft exists.
 */
export function useDraft<T>(key: string, initial: T): UseDraftResult<T> {
  const scope = useDraftScope()
  const storageKey = scope === "pending" ? null : scopedDraftKey(key, scope)

  const [value, setValueState] = useState<T>(initial)
  const [status, setStatus] = useState<DraftStatus>("none")
  const [restoredAt, setRestoredAt] = useState<Date | null>(null)

  // Ref to hold the debounce timer handle.
  const writeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Latest unsaved write; flushed synchronously on unmount / page hide.
  const pendingRef = useRef<{ key: string; value: T } | null>(null)

  function flushPending(): void {
    if (writeTimerRef.current !== null) {
      clearTimeout(writeTimerRef.current)
      writeTimerRef.current = null
    }
    const p = pendingRef.current
    if (!p) return
    pendingRef.current = null
    const record: DraftRecord<T> = { value: p.value, savedAt: new Date().toISOString() }
    draftSet(p.key, record).catch(() => undefined)
  }
  // True once the initial load for the current storageKey has finished. Writes
  // before that are dropped so a mount-time setValue (or a StrictMode remount)
  // can never overwrite the draft that is about to be restored.
  const loadedRef = useRef(false)
  const prevKeyRef = useRef<string | null>(null)
  const flushRef = useRef(flushPending)
  flushRef.current = flushPending

  // On mount (or scope change): attempt to load a draft from IDB.
  useEffect(() => {
    if (typeof window === "undefined") return
    if (storageKey === null) return

    let cancelled = false

    loadedRef.current = false
    if (prevKeyRef.current !== null && prevKeyRef.current !== storageKey) {
      // Account/tenant switched while mounted: never carry the old content over.
      setValueState(initial)
      setStatus("none")
      setRestoredAt(null)
    }
    prevKeyRef.current = storageKey

    const apply = (record: DraftRecord<T>) => {
      setValueState(record.value)
      setStatus("local")
      setRestoredAt(new Date(record.savedAt))
      trackEvent("draft_restore")
    }
    const fresh = (record: DraftRecord<T>) =>
      Date.now() - new Date(record.savedAt).getTime() <= DRAFT_TTL_MS

    async function load(): Promise<void> {
      try {
        await loadInner()
      } finally {
        if (!cancelled) loadedRef.current = true
      }
    }

    async function loadInner(): Promise<void> {
      const record = await draftGet<DraftRecord<T>>(storageKey as string)
      if (cancelled) return
      if (record) {
        if (!fresh(record)) {
          // Draft is too old — discard silently.
          draftDel(storageKey as string).catch(() => undefined)
          return
        }
        apply(record)
        return
      }
      if (storageKey === key) return
      // One-time migration of the pre-scoping key.
      const legacy = await draftGet<DraftRecord<T>>(key)
      if (cancelled || !legacy) return
      if (fresh(legacy)) {
        await draftSet(storageKey as string, legacy)
        if (cancelled) return
        apply(legacy)
      }
      draftDel(key).catch(() => undefined)
    }

    load().catch(() => {
      // IDB unavailable or other error — silently fall back to initial value.
    })

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey])

  // Flush the pending write on page hide / tab switch, and on unmount.
  useEffect(() => {
    const onHide = () => flushRef.current()
    const onVisibility = () => {
      if (document.visibilityState === "hidden") flushRef.current()
    }
    window.addEventListener("pagehide", onHide)
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      window.removeEventListener("pagehide", onHide)
      document.removeEventListener("visibilitychange", onVisibility)
      flushRef.current()
    }
  }, [])

  // Scope/key change: persist the pending write to the previous key first.
  useEffect(() => {
    return () => flushRef.current()
  }, [storageKey])

  function setValue(v: T): void {
    setValueState(v)
    setStatus("local")

    // Session or draft still loading: keep the input in state only (see loadedRef).
    if (storageKey === null || !loadedRef.current) return
    if (writeTimerRef.current !== null) {
      clearTimeout(writeTimerRef.current)
    }
    pendingRef.current = { key: storageKey, value: v }
    writeTimerRef.current = setTimeout(flushPending, WRITE_DEBOUNCE_MS)
  }

  async function markSubmitted(): Promise<void> {
    if (writeTimerRef.current !== null) {
      clearTimeout(writeTimerRef.current)
      writeTimerRef.current = null
    }
    pendingRef.current = null
    setStatus("synced")
    setRestoredAt(null)
    if (storageKey !== null) await draftDel(storageKey)
    if (storageKey !== key) await draftDel(key)
  }

  async function discardDraft(): Promise<void> {
    if (writeTimerRef.current !== null) {
      clearTimeout(writeTimerRef.current)
      writeTimerRef.current = null
    }
    pendingRef.current = null
    setValueState(initial)
    setStatus("none")
    setRestoredAt(null)
    if (storageKey !== null) await draftDel(storageKey)
    if (storageKey !== key) await draftDel(key)
  }

  return { value, setValue, status, markSubmitted, discardDraft, restoredAt }
}
