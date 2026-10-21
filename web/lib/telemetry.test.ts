import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { reportWebError, resetWebErrorDedupe } from "./telemetry"

describe("reportWebError", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    resetWebErrorDedupe()
    fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  it("truncates message and stack", () => {
    reportWebError("render", "m".repeat(500), { stack: "s".repeat(3000) })
    const meta = JSON.parse(fetchMock.mock.calls[0][1].body).metadata
    expect(meta.message).toHaveLength(300)
    expect(meta.stack).toHaveLength(1500)
    expect(meta.kind).toBe("render")
  })
})
