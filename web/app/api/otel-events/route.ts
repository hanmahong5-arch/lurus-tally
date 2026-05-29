import { auth } from "@/auth"
import { NextResponse } from "next/server"

/**
 * Valid event names accepted by this route. Keep in sync with
 * `web/lib/telemetry.ts` TelemetryEvent union AND backend allow-list in
 * `internal/adapter/handler/telemetry/handler.go`.
 *
 * S0.Q3: expanded from 2 → 7 events to back the V1.5 quantitative goals
 * (⌘K DAU ≥60%, AI Drawer DAU ≥30%, plan-accept-rate WAD, onboarding
 * first-PO time, Cmd+Z 5/month).
 */
const VALID_EVENTS = new Set([
  "draft_restore",
  "undo_used",
  "palette_invocation",
  "ai_drawer_open",
  "plan_accept_rate",
  "onboarding_first_po_exported",
  "cmd_z_used",
])

interface OtelEventPayload {
  event?: unknown
  metadata?: unknown
}

/**
 * POST /api/otel-events
 *
 * Accepts: { event: TelemetryEvent; metadata?: Record<string, unknown> }
 * Returns: { ok: true } on success, { ok: false, error: string } on validation failure.
 *
 * Forwarding:
 *   - OTEL_COLLECTOR_URL set → POST to OTel collector (legacy path).
 *   - TALLY_BACKEND_TELEMETRY_URL set → POST to Tally backend
 *     `/internal/v1/telemetry/web`, which forwards to NATS
 *     `PSI_TELEMETRY.web.<event>` (S0.Q3 new path).
 *   - Both set → both are notified; failures are swallowed independently.
 *   - Neither set → no-op, returns 200 (safe for local dev).
 */
export async function POST(request: Request): Promise<NextResponse> {
  let body: OtelEventPayload
  try {
    body = (await request.json()) as OtelEventPayload
  } catch {
    return NextResponse.json({ ok: false, error: "invalid JSON body" }, { status: 400 })
  }

  const { event, metadata } = body

  if (!event || typeof event !== "string" || !VALID_EVENTS.has(event)) {
    return NextResponse.json(
      { ok: false, error: "missing or invalid event field" },
      { status: 400 }
    )
  }

  const attrs = typeof metadata === "object" && metadata !== null ? metadata : {}

  const sinks: Promise<unknown>[] = []

  const collectorUrl = process.env.OTEL_COLLECTOR_URL
  if (collectorUrl) {
    sinks.push(
      fetch(collectorUrl, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: event, attributes: attrs }),
      }).catch(() => undefined)
    )
  }

  const backendUrl = process.env.TALLY_BACKEND_TELEMETRY_URL
  if (backendUrl) {
    const internalKey = process.env.PLATFORM_INTERNAL_KEY ?? ""
    // Inject the *verified* server-side identity so the backend can build
    // per-user DAU. Never trust a client-supplied id — telemetry fires from the
    // browser and would be trivially spoofable. Omitted when there is no
    // session (pre-login events) so the backend degrades to not-counted rather
    // than attributing the hit to a fake user.
    const session = await auth()
    const identity: Record<string, string> = {}
    if (session?.user?.id) identity.user_id = session.user.id
    if (session?.user?.tenantId) identity.tenant_id = session.user.tenantId
    sinks.push(
      fetch(backendUrl, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(internalKey ? { Authorization: `Bearer ${internalKey}` } : {}),
        },
        body: JSON.stringify({ event, metadata: attrs, ...identity }),
      }).catch(() => undefined)
    )
  }

  await Promise.all(sinks)
  return NextResponse.json({ ok: true })
}
