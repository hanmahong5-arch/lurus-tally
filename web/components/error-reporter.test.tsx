import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, cleanup } from "@testing-library/react"
import { ErrorReporter } from "./error-reporter"
import { resetWebErrorDedupe } from "@/lib/telemetry"

function sent(fetchMock: ReturnType<typeof vi.fn>) {
  return fetchMock.mock.calls.map((c) => JSON.parse((c[1] as RequestInit).body as string))
}

describe("ErrorReporter", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    vi.useFakeTimers()
    resetWebErrorDedupe()
    fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it("reports the same message once per 60s", () => {
    render(<ErrorReporter />)
    const fire = () =>
      window.dispatchEvent(new ErrorEvent("error", { message: "boom", error: new Error("boom") }))
    fire()
    fire()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const body = sent(fetchMock)[0]
    expect(body.event).toBe("web_error")
    expect(body.metadata.kind).toBe("unhandled")
    vi.advanceTimersByTime(61_000)
    fire()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("reports unhandled rejections", () => {
    render(<ErrorReporter />)
    const ev = new Event("unhandledrejection") as Event & { reason: unknown }
    ev.reason = new Error("nope")
    window.dispatchEvent(ev)
    expect(sent(fetchMock)[0].metadata).toMatchObject({ kind: "rejection", message: "nope" })
  })
})
