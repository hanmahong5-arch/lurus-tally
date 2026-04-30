/**
 * Fire-and-forget telemetry helper.
 *
 * Sends a lightweight event to the internal OTel route handler.
 * All errors are swallowed — telemetry must never break the UI.
 */

export type TelemetryEvent = "draft_restore" | "undo_used"

/**
 * trackEvent sends an event to /api/otel-events.
 * It is fire-and-forget: the returned Promise is never awaited by callers.
 */
export function trackEvent(
  event: TelemetryEvent,
  metadata?: Record<string, string>
): void {
  // Safety: do not run during SSR.
  if (typeof window === "undefined") return

  fetch("/api/otel-events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ event, metadata }),
  }).catch(() => undefined)
}
