"use client"

import { useCallback, useEffect, useState } from "react"

/**
 * Whether the AI assistant can be used by this shop, learned from a cheap GET
 * on mount instead of from the first failed question.
 *
 *   - 503 { error: "ai_not_configured" } : the service has no AI wired
 *   - 403 { error: "ai_not_entitled" }   : the shop's plan does not include it
 *
 * Both show the same friendly empty state (with an upgrade entry for the
 * second), never an "错误" bubble. Any other outcome (network trouble, 5xx,
 * 401) is "unknown": the UI stays usable and a real send reports its own error.
 */
export type AIAvailability = "unknown" | "available" | "not_configured" | "not_entitled"

export const AI_NOT_CONFIGURED_ERR = "ai_not_configured"
export const AI_NOT_ENTITLED_ERR = "ai_not_entitled"

const PROBE_URL = "/api/proxy/ai/plans?status=pending"

/** Maps an AI error code (HTTP body `error`) to an unavailable state, or null. */
export function unavailableReason(code: string | undefined | null): "not_configured" | "not_entitled" | null {
  if (code === AI_NOT_CONFIGURED_ERR) return "not_configured"
  if (code === AI_NOT_ENTITLED_ERR) return "not_entitled"
  return null
}

/** Calls the probe once and classifies the answer. Never throws. */
export async function probeAIAvailability(signal?: AbortSignal): Promise<AIAvailability> {
  try {
    const resp = await fetch(PROBE_URL, { method: "GET", signal })
    if (resp.ok) return "available"
    const body = (await resp.json().catch(() => ({}))) as { error?: string }
    return unavailableReason(body.error) ?? "unknown"
  } catch {
    return "unknown"
  }
}

/**
 * useAIAvailability probes once on mount. `markUnavailable` lets a failed send
 * (the probe raced a plan change, say) flip the same state.
 */
export function useAIAvailability(): {
  availability: AIAvailability
  markUnavailable: (reason: "not_configured" | "not_entitled") => void
} {
  const [availability, setAvailability] = useState<AIAvailability>("unknown")

  useEffect(() => {
    const ctrl = new AbortController()
    probeAIAvailability(ctrl.signal).then((a) => {
      if (!ctrl.signal.aborted) setAvailability((prev) => (prev === "unknown" ? a : prev))
    })
    return () => ctrl.abort()
  }, [])

  const markUnavailable = useCallback((reason: "not_configured" | "not_entitled") => {
    setAvailability(reason)
  }, [])

  return { availability, markUnavailable }
}
