import { NextResponse } from "next/server"

/** Valid event names accepted by this route. */
const VALID_EVENTS = new Set(["draft_restore", "undo_used"])

interface OtelEventPayload {
  event?: unknown
  metadata?: unknown
}

/**
 * POST /api/otel-events
 *
 * Accepts: { event: 'draft_restore' | 'undo_used'; metadata?: Record<string, string> }
 * Returns: { ok: true } on success, { ok: false, error: string } on validation failure.
 *
 * When OTEL_COLLECTOR_URL is unset, the route is a no-op (returns 200 immediately).
 * This keeps the handler safe in local dev where no collector is running.
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

  const collectorUrl = process.env.OTEL_COLLECTOR_URL
  if (!collectorUrl) {
    // No-op in local dev — collector is not configured.
    return NextResponse.json({ ok: true })
  }

  // Forward the event to the OTel collector (best-effort, fire-and-forget).
  try {
    await fetch(collectorUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: event,
        attributes: typeof metadata === "object" && metadata !== null ? metadata : {},
      }),
    })
  } catch {
    // Collector unreachable — still return 200 to the client; telemetry is non-critical.
  }

  return NextResponse.json({ ok: true })
}
