import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"

// The Next.js route handler uses NextResponse which requires the next/server module.
// In vitest/jsdom we mock fetch for the outbound collector call.

// Mock the NextAuth server helper so the route never pulls in the real auth
// stack (Zitadel/OIDC) under jsdom. authMock controls the "current session".
const authMock = vi.fn<() => Promise<unknown>>(() => Promise.resolve(null))
vi.mock("@/auth", () => ({ auth: () => authMock() }))

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
    delete process.env.OTEL_COLLECTOR_URL
    delete process.env.TALLY_BACKEND_TELEMETRY_URL
    delete process.env.PLATFORM_INTERNAL_KEY
    authMock.mockReset()
    authMock.mockResolvedValue(null)
    vi.resetModules()
  })

  afterEach(() => {
    delete process.env.OTEL_COLLECTOR_URL
    delete process.env.TALLY_BACKEND_TELEMETRY_URL
    delete process.env.PLATFORM_INTERNAL_KEY
    vi.resetModules()
    vi.restoreAllMocks()
  })

  it("legacy: accepts draft_restore", async () => {
    const res = await callRoute({ event: "draft_restore" })
    expect(res.status).toBe(200)
    expect((await res.json()).ok).toBe(true)
  })

  it("legacy: accepts undo_used with metadata", async () => {
    const res = await callRoute({ event: "undo_used", metadata: { entity_type: "bill" } })
    expect(res.status).toBe(200)
  })

  it("rejects missing event", async () => {
    const res = await callRoute({})
    expect(res.status).toBe(400)
    expect((await res.json()).ok).toBe(false)
  })

  it("rejects unknown event", async () => {
    const res = await callRoute({ event: "totally_made_up" })
    expect(res.status).toBe(400)
  })

  // S0.Q3: each new event is in the allow-list.
  for (const event of [
    "palette_invocation",
    "ai_drawer_open",
    "plan_accept_rate",
    "onboarding_first_po_exported",
    "cmd_z_used",
    // North Star WAD: was previously absent from VALID_EVENTS → 400-rejected.
    "wad_increment",
  ]) {
    it(`S0.Q3: accepts ${event}`, async () => {
      const res = await callRoute({ event, metadata: { sample: "value" } })
      expect(res.status).toBe(200)
    })
  }

  it("S0.Q3: forwards to backend telemetry URL when set", async () => {
    process.env.TALLY_BACKEND_TELEMETRY_URL = "http://backend.test/internal/v1/telemetry/web"
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(
      () => Promise.resolve(new Response("ok", { status: 200 }))
    )
    vi.stubGlobal("fetch", fetchMock)

    const res = await callRoute({
      event: "palette_invocation",
      metadata: { latency_ms: 123, query_chars: 4, action_picked: "navigate" },
    })

    expect(res.status).toBe(200)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const callArg = fetchMock.mock.calls[0]?.[0]
    expect(callArg).toBe("http://backend.test/internal/v1/telemetry/web")
  })

  it("S0.Q3: adds Authorization header when PLATFORM_INTERNAL_KEY set", async () => {
    process.env.TALLY_BACKEND_TELEMETRY_URL = "http://backend.test/internal/v1/telemetry/web"
    process.env.PLATFORM_INTERNAL_KEY = "test-secret"
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(
      () => Promise.resolve(new Response("ok", { status: 200 }))
    )
    vi.stubGlobal("fetch", fetchMock)

    await callRoute({
      event: "ai_drawer_open",
      metadata: { page_context: "/products", trigger: "shortcut" },
    })

    const init = fetchMock.mock.calls[0]?.[1]
    const headers = (init?.headers ?? {}) as Record<string, string>
    expect(headers.Authorization).toBe("Bearer test-secret")
  })

  it("S0.Q3: swallows backend forward failure (UI must not break)", async () => {
    process.env.TALLY_BACKEND_TELEMETRY_URL = "http://backend.test/internal/v1/telemetry/web"
    vi.stubGlobal("fetch", vi.fn(() => Promise.reject(new Error("net down"))))

    const res = await callRoute({ event: "cmd_z_used", metadata: { entity_type: "bill", undo_latency_ms: 18 } })
    expect(res.status).toBe(200)
  })

  it("S0.C1: injects verified user_id and tenant_id into the backend body", async () => {
    process.env.TALLY_BACKEND_TELEMETRY_URL = "http://backend.test/internal/v1/telemetry/web"
    authMock.mockResolvedValue({ user: { id: "user-123", tenantId: "tenant-abc" } })
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(
      () => Promise.resolve(new Response("ok", { status: 200 }))
    )
    vi.stubGlobal("fetch", fetchMock)

    await callRoute({
      event: "palette_invocation",
      metadata: { latency_ms: 50, query_chars: 2, action_picked: "navigate" },
    })

    const init = fetchMock.mock.calls[0]?.[1]
    const body = JSON.parse(String(init?.body)) as Record<string, unknown>
    expect(body.user_id).toBe("user-123")
    expect(body.tenant_id).toBe("tenant-abc")
    expect(body.event).toBe("palette_invocation")
  })

  it("S0.C1: omits identity when there is no session", async () => {
    process.env.TALLY_BACKEND_TELEMETRY_URL = "http://backend.test/internal/v1/telemetry/web"
    authMock.mockResolvedValue(null)
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(
      () => Promise.resolve(new Response("ok", { status: 200 }))
    )
    vi.stubGlobal("fetch", fetchMock)

    await callRoute({ event: "ai_drawer_open", metadata: { page_context: "/", trigger: "button" } })

    const init = fetchMock.mock.calls[0]?.[1]
    const body = JSON.parse(String(init?.body)) as Record<string, unknown>
    expect(body).not.toHaveProperty("user_id")
    expect(body).not.toHaveProperty("tenant_id")
  })
})
