import { afterEach, describe, expect, it, vi } from "vitest"
import { streamChat, isAINotConfigured } from "./ai"

afterEach(() => {
  vi.unstubAllGlobals()
})

const mkCallbacks = () => ({
  onChunk: vi.fn(),
  onPlan: vi.fn(),
  onDone: vi.fn(),
  onError: vi.fn(),
  onNotConfigured: vi.fn(),
})

describe("streamChat ai_not_configured", () => {
  it("routes the stable code to onNotConfigured, not onError", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "ai_not_configured", message: "AI 助手尚未开通" }), {
        status: 503,
      })
    )
    vi.stubGlobal("fetch", fetchMock)
    const cb = mkCallbacks()
    streamChat("hi", [], cb)
    await vi.waitFor(() => expect(cb.onNotConfigured).toHaveBeenCalledTimes(1))
    expect(cb.onError).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(1) // no retry
  })

  it("other HTTP failures still go to onError", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "boom" }), { status: 500 })))
    const cb = mkCallbacks()
    streamChat("hi", [], cb)
    await vi.waitFor(() => expect(cb.onError).toHaveBeenCalledWith("boom"))
    expect(cb.onNotConfigured).not.toHaveBeenCalled()
  })

  it("isAINotConfigured matches only the exact code", () => {
    expect(isAINotConfigured({ error: "ai_not_configured" })).toBe(true)
    expect(isAINotConfigured({ error: "other" })).toBe(false)
    expect(isAINotConfigured(undefined)).toBe(false)
  })
})
