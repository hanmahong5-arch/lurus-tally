import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { renderHook, act } from "@testing-library/react"
import { useReadyOrTimeout } from "./use-print-data"

describe("useReadyOrTimeout", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("does not start the clock until the document has loaded", () => {
    const { result, rerender } = renderHook(({ started }) => useReadyOrTimeout(false, started, 4000), {
      initialProps: { started: false },
    })
    act(() => {
      vi.advanceTimersByTime(10000)
    })
    expect(result.current).toBe(false)
    rerender({ started: true })
    act(() => {
      vi.advanceTimersByTime(3999)
    })
    expect(result.current).toBe(false)
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(result.current).toBe(true)
  })

  it("is ready at once when everything is done", () => {
    const { result } = renderHook(() => useReadyOrTimeout(true, true))
    expect(result.current).toBe(true)
  })
})
