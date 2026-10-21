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
  | "wad_increment"
  | "web_error"

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

  /**
   * WAD (Weekly Active Decision) increment — one purchase draft generated
   * from the replenishment suggestion panel.
   * North Star metric: accumulate ≥ 80 WAD in 90 days.
   */
  wad_increment: {
    /** Number of purchase drafts created in this batch (one per supplier group). */
    draft_count: number
    /** Number of product lines included across all drafts. */
    line_count: number
  }

  /** Browser-side error, logged by the backend so support can search by tenant. */
  web_error: {
    route: string
    /** Truncated to 300 chars. Never put request bodies or user input here. */
    message: string
    /** Truncated to 1500 chars. */
    stack?: string
    kind: "render" | "unhandled" | "rejection" | "api"
    /** Backend request id (api kind only). */
    request_id?: string
    /** Request path without query string (api kind only). */
    path?: string
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

const ERR_DEDUP_MS = 60_000
const MSG_MAX = 300
const STACK_MAX = 1500
const recentErrors = new Map<string, number>()

/** Test hook: forget the dedupe window. */
export function resetWebErrorDedupe(): void {
  recentErrors.clear()
}

/**
 * reportWebError sends a web_error event. The same message is reported at most
 * once per 60 seconds; browser only; never throws.
 */
export function reportWebError(
  kind: TelemetryMetadata["web_error"]["kind"],
  message: string,
  extra: { stack?: string; request_id?: string; path?: string } = {}
): void {
  if (typeof window === "undefined") return
  try {
    const msg = (message || "unknown").slice(0, MSG_MAX)
    const now = Date.now()
    const last = recentErrors.get(msg)
    if (last !== undefined && now - last < ERR_DEDUP_MS) return
    if (recentErrors.size > 200) recentErrors.clear()
    recentErrors.set(msg, now)
    const meta: TelemetryMetadata["web_error"] = {
      route: window.location?.pathname ?? "",
      message: msg,
      kind,
    }
    if (extra.stack) meta.stack = extra.stack.slice(0, STACK_MAX)
    if (extra.request_id) meta.request_id = extra.request_id
    if (extra.path) meta.path = extra.path
    trackEvent("web_error", meta)
  } catch {
    // telemetry must never break the UI
  }
}
