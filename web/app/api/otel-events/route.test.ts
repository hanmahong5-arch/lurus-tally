import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"

// The Next.js route handler uses NextResponse which requires the next/server module.
// In vitest/jsdom we mock fetch for the outbound collector call.

// Dynamic import of the route handler (avoids ESM issues with NextResponse at top level).
async function callRoute(body: unknown): Promise<Response> {
  const { POST } = await import("./route")
  const request = new Request("http://localhost/api/otel-events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  return POST(request)
}

describe("POST /api/otel-events", () => {
  beforeEach(() => {
    // Ensure OTEL_COLLECTOR_URL is not set so the route acts as no-op.
    delete process.env.OTEL_COLLECTOR_URL
    vi.resetModules()
  })

  afterEach(() => {
    delete process.env.OTEL_COLLECTOR_URL
    vi.resetModules()
  })

  it("TestOtelEventsRoute_ValidPayload_Returns200", async () => {
    const res = await callRoute({ event: "draft_restore" })
    expect(res.status).toBe(200)
    const json = await res.json()
    expect(json.ok).toBe(true)
  })

  it("TestOtelEventsRoute_InvalidPayload_Returns400", async () => {
    const res = await callRoute({})
    expect(res.status).toBe(400)
    const json = await res.json()
    expect(json.ok).toBe(false)
  })

  it("TestOtelEventsRoute_UnknownEvent_Returns400", async () => {
    const res = await callRoute({ event: "unknown_event" })
    expect(res.status).toBe(400)
  })

  it("TestOtelEventsRoute_ValidUndoUsedEvent_Returns200", async () => {
    const res = await callRoute({ event: "undo_used", metadata: { page: "products" } })
    expect(res.status).toBe(200)
    const json = await res.json()
    expect(json.ok).toBe(true)
  })
})
