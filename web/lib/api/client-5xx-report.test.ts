import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { apiFetch } from "./client"
import { resetWebErrorDedupe } from "@/lib/telemetry"

describe("apiFetch 5xx reporting", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    resetWebErrorDedupe()
    vi.spyOn(console, "error").mockImplementation(() => undefined)
    fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url === "/api/otel-events") return Promise.resolve({ ok: true })
      return Promise.resolve(
        new Response(JSON.stringify({ error: "x" }), {
          status: 500,
          headers: { "x-request-id": "req-9" },
        })
      )
    })
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("reports kind=api with request id and path, no body", async () => {
    await expect(
      apiFetch("/api/v1/x?secret=1", {
        method: "POST",
        body: JSON.stringify({ pw: "hunter2" }),
        silent: true,
      })
    ).rejects.toBeTruthy()
    const call = fetchMock.mock.calls.find((c) => c[0] === "/api/otel-events")!
    const raw = call[1].body as string
    const meta = JSON.parse(raw).metadata
    expect(meta).toMatchObject({ kind: "api", request_id: "req-9", path: "/api/proxy/api/v1/x" })
    expect(raw).not.toContain("hunter2")
    expect(raw).not.toContain("secret")
  })
})
