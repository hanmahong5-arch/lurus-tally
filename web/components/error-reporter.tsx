"use client"

import { useEffect } from "react"
import { reportWebError } from "@/lib/telemetry"

/**
 * Listens for uncaught browser errors and unhandled promise rejections and
 * forwards them to the backend log (deduped per message for 60s inside
 * reportWebError). Renders nothing.
 */
export function ErrorReporter() {
  useEffect(() => {
    const onError = (e: ErrorEvent) => {
      reportWebError("unhandled", e.message || e.error?.message || "unknown", {
        stack: e.error?.stack,
      })
    }
    const onRejection = (e: PromiseRejectionEvent) => {
      const r = e.reason
      const msg = r instanceof Error ? r.message : typeof r === "string" ? r : "unhandled rejection"
      reportWebError("rejection", msg, { stack: r instanceof Error ? r.stack : undefined })
    }
    window.addEventListener("error", onError)
    window.addEventListener("unhandledrejection", onRejection)
    return () => {
      window.removeEventListener("error", onError)
      window.removeEventListener("unhandledrejection", onRejection)
    }
  }, [])
  return null
}
