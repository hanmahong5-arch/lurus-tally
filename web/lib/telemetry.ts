/**
 * Fire-and-forget telemetry helper.
 *
 * Sends a lightweight event to the internal OTel route handler.
 * All errors are swallowed — telemetry must never break the UI.
 *
 * S0.Q3 (roadmap-v1.5): 5 new events added on top of the original 2.
 * Each event carries a typed metadata payload, declared via the
 * `TelemetryMetadata` map below. Backend allow-list at
 * `/internal/v1/telemetry/web` mirrors this set — keep them in sync.
 */

export type TelemetryEvent =
  | "draft_restore"
  | "undo_used"
  | "palette_invocation"
  | "ai_drawer_open"
  | "plan_accept_rate"
  | "onboarding_first_po_exported"
  | "cmd_z_used"

/**
 * TelemetryMetadata maps each event to the shape of its `metadata` argument.
 *
 * PII rule: metadata fields are sent to NATS / OTel and may be retained.
 * Never include free-text user input, email, note, or supplier name. Stick
 * to enums, IDs, latency numbers, and short categorical values.
 */
export interface TelemetryMetadata {
  draft_restore: Record<string, string>
  undo_used: { entity_type?: string }

  /** ⌘K palette invocation. */
  palette_invocation: {
    /** End-to-end latency from keystroke to first row rendered. */
    latency_ms: number
    /** Characters typed before the user picked / closed. */
    query_chars: number
    /** Which row the user accepted, or `none` if dismissed. */
    action_picked: "navigate" | "query" | "execute" | "none"
  }

  /** AI Drawer opened. */
  ai_drawer_open: {
    /** Route the user was on when the drawer opened. */
    page_context: string
    /** How the drawer was triggered. */
    trigger: "shortcut" | "button" | "deeplink"
  }

  /** AI Plan accepted or rejected. Used to derive plan_accept_rate WAD metric. */
  plan_accept_rate: {
    plan_id: string
    kind: "replenishment" | "movement" | "transfer" | "other"
    /** "1" = accepted, "0" = rejected. String to keep label cardinality bounded. */
    accepted: "1" | "0"
  }

  /** First "export PO" click after signup — the onboarding aha moment. */
  onboarding_first_po_exported: {
    /** Minutes since tenant signup. Drives KS1 onboarding-completion-rate. */
    tenant_age_minutes: number
    export_format: "csv" | "pdf"
  }

  /** Time Machine / Cmd+Z invocation. */
  cmd_z_used: {
    entity_type: string
    undo_latency_ms: number
  }
}

/**
 * trackEvent sends an event to /api/otel-events.
 * It is fire-and-forget: the returned Promise is never awaited by callers.
 */
export function trackEvent<E extends TelemetryEvent>(
  event: E,
  metadata?: TelemetryMetadata[E]
): void {
  // Safety: do not run during SSR.
  if (typeof window === "undefined") return

  // Wrap in try/catch as well as .catch — a missing/broken fetch must never
  // throw synchronously into a UI handler (telemetry is fire-and-forget).
  try {
    void fetch("/api/otel-events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ event, metadata }),
    }).catch(() => undefined)
  } catch {
    // ignore
  }
}
