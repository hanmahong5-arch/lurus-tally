import { describe, it, expect, vi, afterEach } from "vitest"
import { probeAIAvailability, unavailableReason } from "./ai-availability"

function stubFetch(status: number, body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })),
  )
}

describe("probeAIAvailability", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("200 -> available", async () => {
    stubFetch(200, { items: [], count: 0 })
    expect(await probeAIAvailability()).toBe("available")
  })
  it("403 ai_not_entitled -> not_entitled", async () => {
    stubFetch(403, { error: "ai_not_entitled" })
    expect(await probeAIAvailability()).toBe("not_entitled")
  })
  it("503 ai_not_configured -> not_configured", async () => {
    stubFetch(503, { error: "ai_not_configured" })
    expect(await probeAIAvailability()).toBe("not_configured")
  })
  it("other failures stay unknown so the UI remains usable", async () => {
    stubFetch(500, { error: "internal" })
    expect(await probeAIAvailability()).toBe("unknown")
    stubFetch(401, { error: "unauthorized" })
    expect(await probeAIAvailability()).toBe("unknown")
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")))
    expect(await probeAIAvailability()).toBe("unknown")
  })
  it("unavailableReason maps only the two stable codes", () => {
    expect(unavailableReason("ai_not_entitled")).toBe("not_entitled")
    expect(unavailableReason("ai_not_configured")).toBe("not_configured")
    expect(unavailableReason("feature_not_in_plan")).toBeNull()
    expect(unavailableReason(undefined)).toBeNull()
  })
})
