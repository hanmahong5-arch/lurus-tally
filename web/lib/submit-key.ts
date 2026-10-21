"use client"

import { useCallback, useRef } from "react"
import { ApiError, NetworkError } from "@/lib/api/errors"

/**
 * Idempotency-Key holder for a single "submit" button.
 *
 * The server replays the cached response for a repeated key only for 2xx, and
 * rejects a key reused with a different body (422), so the key may only be
 * reused when the first attempt got no response and the payload is unchanged:
 *   - NetworkError / gateway 502-504 (no real answer) -> keep the key, a resubmit is a true retry
 *   - any HTTP response / success -> drop the key
 *   - payload changed             -> new key
 */
export function useSubmitKey() {
  const held = useRef<{ key: string; fingerprint: string } | null>(null)

  /** Key for this attempt; stable while the payload is unchanged and a retry is pending. */
  const keyFor = useCallback((payload: unknown): string => {
    const fingerprint = JSON.stringify(payload)
    if (!held.current || held.current.fingerprint !== fingerprint) {
      held.current = { key: newKey(), fingerprint }
    }
    return held.current.key
  }, [])

  /** Call with whatever the submit threw; keeps the key only for a lost response. */
  const failed = useCallback((err: unknown) => {
    // A gateway 502/503/504 may mean the write landed upstream: treat it as a lost reply too.
    const lost =
      err instanceof NetworkError ||
      (err instanceof ApiError && (err.status === 502 || err.status === 503 || err.status === 504))
    if (!lost) held.current = null
  }, [])

  /** Call after a successful submit. */
  const succeeded = useCallback(() => {
    held.current = null
  }, [])

  return { keyFor, failed, succeeded }
}

function newKey(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID()
  }
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 12)
}
