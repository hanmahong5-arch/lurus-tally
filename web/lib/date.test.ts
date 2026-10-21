import { describe, it, expect, vi, afterEach } from "vitest"
import { todayCST } from "./date"

describe("todayCST", () => {
  afterEach(() => vi.useRealTimers())

  it("北京时间 07:00（UTC 前一天 23:00）仍是当天", () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2026-10-09T23:00:00Z"))
    expect(todayCST()).toBe("2026-10-10")
  })

  it("北京时间 23:30（UTC 15:30）还是当天", () => {
    expect(todayCST(new Date("2026-10-10T15:30:00Z"))).toBe("2026-10-10")
  })

  it("跨过北京时间零点进入下一天", () => {
    expect(todayCST(new Date("2026-10-10T16:00:00Z"))).toBe("2026-10-11")
  })
})
