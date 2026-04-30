import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"

// We test the non-streaming helpers (confirmPlan, cancelPlan) by mocking fetch.
// The SSE streaming function (streamChat) is covered by integration tests only
// since JSDOM has no real ReadableStream from fetch.

describe("AI API client", () => {
  const originalFetch = global.fetch

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    global.fetch = originalFetch
  })

  it("TestConfirmPlan_Success_ResolvesWithoutError", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ plan_id: "plan-123", status: "confirmed" }),
    } as Response)

    const { confirmPlan } = await import("./ai")
    await expect(confirmPlan("plan-123")).resolves.toBeUndefined()

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining("ai/plans/plan-123/confirm"),
      expect.objectContaining({ method: "POST" })
    )
  })

  it("TestConfirmPlan_NotFound_ThrowsError", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({ error: "not_found", detail: "plan not found or expired" }),
    } as Response)

    const { confirmPlan } = await import("./ai")
    await expect(confirmPlan("missing-plan")).rejects.toThrow("plan not found or expired")
  })

  it("TestCancelPlan_Success_ResolvesWithoutError", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: "cancelled" }),
    } as Response)

    const { cancelPlan } = await import("./ai")
    await expect(cancelPlan("plan-456")).resolves.toBeUndefined()

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining("ai/plans/plan-456/cancel"),
      expect.objectContaining({ method: "POST" })
    )
  })

  it("TestCancelPlan_ServerError_ThrowsError", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: "internal_error", detail: "boom" }),
    } as Response)

    const { cancelPlan } = await import("./ai")
    await expect(cancelPlan("plan-789")).rejects.toThrow("boom")
  })
})
