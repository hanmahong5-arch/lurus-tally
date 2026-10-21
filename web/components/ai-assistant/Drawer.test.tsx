import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, act } from "@testing-library/react"
import { AIDrawer } from "./Drawer"

const streamChat = vi.fn()

vi.mock("@/lib/api/ai", async (importOriginal) => {
  const mod = await importOriginal<typeof import("@/lib/api/ai")>()
  return { ...mod, streamChat: (...args: unknown[]) => streamChat(...args) }
})
vi.mock("@/lib/telemetry", () => ({ trackEvent: vi.fn() }))

type Callbacks = {
  onChunk: (c: string) => void
  onDone: () => void
  onError: (e: string) => void
  onNotConfigured?: () => void
}

function mockProbe(status: number, body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  )
  vi.stubGlobal("fetch", fetchMock)
  return fetchMock
}

function fireQuery(query: string) {
  act(() => {
    window.dispatchEvent(new CustomEvent("tally:ai-query", { detail: { query } }))
  })
}

describe("AIDrawer", () => {
  beforeEach(() => {
    // jsdom has no scrollIntoView; the drawer scrolls on every message change.
    Element.prototype.scrollIntoView = vi.fn()
    streamChat.mockReset()
    streamChat.mockImplementation((_q: string, _h: unknown, cb: Callbacks) => {
      queueMicrotask(() => cb.onDone())
      return () => {}
    })
    window.localStorage.clear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("sends the ⌘K query to /ai/chat exactly once", async () => {
    mockProbe(200, { items: [], count: 0 })
    render(<AIDrawer />)
    fireQuery("哪些商品快断货了")
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
    expect(streamChat.mock.calls[0][0]).toBe("哪些商品快断货了")
    // Give any stray re-scheduled timer time to fire: still exactly one call.
    await new Promise((r) => setTimeout(r, 500))
    expect(streamChat).toHaveBeenCalledTimes(1)
  })

  it("probes /ai/plans once on mount", async () => {
    const fetchMock = mockProbe(200, { items: [], count: 0 })
    render(<AIDrawer />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(String(fetchMock.mock.calls[0][0])).toContain("/api/proxy/ai/plans")
  })

  it("shows the not-enabled empty state with an upgrade entry for ai_not_entitled, no error bubble", async () => {
    mockProbe(403, { error: "ai_not_entitled", message: "当前套餐不包含此功能，请升级套餐" })
    render(<AIDrawer />)
    const empty = await screen.findByTestId("ai-not-configured")
    expect(empty.getAttribute("data-reason")).toBe("not_entitled")
    expect(empty.textContent).toContain("试用期暂未开放")
    expect(screen.queryByTestId("ai-upgrade-link")).toBeNull()
    expect(screen.queryByText(/错误/)).toBeNull()
    // And a ⌘K query is not sent.
    fireQuery("本月销售额")
    await new Promise((r) => setTimeout(r, 400))
    expect(streamChat).not.toHaveBeenCalled()
  })

  it("shows the empty state (no upgrade link) for ai_not_configured", async () => {
    mockProbe(503, { error: "ai_not_configured" })
    render(<AIDrawer />)
    const empty = await screen.findByTestId("ai-not-configured")
    expect(empty.getAttribute("data-reason")).toBe("not_configured")
    expect(screen.queryByTestId("ai-upgrade-link")).toBeNull()
  })

  it("turns a send-time ai_not_entitled into the empty state, not an 错误 bubble", async () => {
    mockProbe(500, {}) // probe inconclusive
    streamChat.mockImplementation((_q: string, _h: unknown, cb: Callbacks) => {
      queueMicrotask(() => cb.onError("ai_not_entitled"))
      return () => {}
    })
    render(<AIDrawer />)
    fireQuery("本月销售额")
    const empty = await screen.findByTestId("ai-not-configured")
    expect(empty.getAttribute("data-reason")).toBe("not_entitled")
    expect(screen.queryByText(/错误/)).toBeNull()
  })
})
