import { describe, it, expect } from "vitest"
import { resolveDateRange, shanghaiToday } from "./date-range"

// 2026-03-15 12:00 Shanghai == 04:00Z
const at = (iso: string) => new Date(iso)

describe("shanghaiToday", () => {
  it("rolls over at Shanghai midnight, not UTC", () => {
    expect(shanghaiToday(at("2026-03-14T16:30:00Z"))).toBe("2026-03-15")
    expect(shanghaiToday(at("2026-03-14T15:59:00Z"))).toBe("2026-03-14")
  })
})

describe("resolveDateRange", () => {
  it("all has no bounds", () => {
    expect(resolveDateRange("all", at("2026-03-15T04:00:00Z"))).toEqual({ from: "", to: "" })
  })

  it("today", () => {
    expect(resolveDateRange("today", at("2026-03-15T04:00:00Z"))).toEqual({ from: "2026-03-15", to: "2026-03-15" })
  })

  it("this_week starts on Monday", () => {
    // 2026-03-15 is a Sunday -> week is 03-09..03-15
    expect(resolveDateRange("this_week", at("2026-03-15T04:00:00Z"))).toEqual({ from: "2026-03-09", to: "2026-03-15" })
    // Monday itself
    expect(resolveDateRange("this_week", at("2026-03-09T04:00:00Z"))).toEqual({ from: "2026-03-09", to: "2026-03-15" })
    // Monday 00:30 Shanghai while UTC is still Sunday
    expect(resolveDateRange("this_week", at("2026-03-08T16:30:00Z"))).toEqual({ from: "2026-03-09", to: "2026-03-15" })
  })

  it("this_week crosses a year boundary", () => {
    // 2027-01-01 is a Friday -> Monday is 2026-12-28
    expect(resolveDateRange("this_week", at("2027-01-01T04:00:00Z"))).toEqual({ from: "2026-12-28", to: "2027-01-03" })
  })

  it("this_month ends on the last day of the month", () => {
    expect(resolveDateRange("this_month", at("2026-01-10T04:00:00Z"))).toEqual({ from: "2026-01-01", to: "2026-01-31" })
    expect(resolveDateRange("this_month", at("2026-02-10T04:00:00Z"))).toEqual({ from: "2026-02-01", to: "2026-02-28" })
    expect(resolveDateRange("this_month", at("2028-02-10T04:00:00Z"))).toEqual({ from: "2028-02-01", to: "2028-02-29" })
    expect(resolveDateRange("this_month", at("2026-12-31T04:00:00Z"))).toEqual({ from: "2026-12-01", to: "2026-12-31" })
  })

  it("last_month crosses the year boundary", () => {
    expect(resolveDateRange("last_month", at("2026-01-10T04:00:00Z"))).toEqual({ from: "2025-12-01", to: "2025-12-31" })
  })

  it("last_month handles a shorter previous month", () => {
    expect(resolveDateRange("last_month", at("2026-03-31T04:00:00Z"))).toEqual({ from: "2026-02-01", to: "2026-02-28" })
  })

  it("custom returns the supplied bounds", () => {
    expect(resolveDateRange("custom", at("2026-03-15T04:00:00Z"), { from: "2026-01-02", to: "2026-01-09" })).toEqual({
      from: "2026-01-02",
      to: "2026-01-09",
    })
  })

  it("custom without bounds is empty", () => {
    expect(resolveDateRange("custom", at("2026-03-15T04:00:00Z"))).toEqual({ from: "", to: "" })
  })
})
