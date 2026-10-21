import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, cleanup } from "@testing-library/react"
import RootError from "./error"
import { resetWebErrorDedupe } from "@/lib/telemetry"

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }))

describe("RootError", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    resetWebErrorDedupe()
    fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal("fetch", fetchMock)
    vi.spyOn(console, "error").mockImplementation(() => undefined)
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("reports a render error once", () => {
    const err = new Error("render boom")
    const { rerender } = render(<RootError error={err} reset={() => {}} />)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).metadata.kind).toBe("render")
    rerender(<RootError error={new Error("render boom")} reset={() => {}} />)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
