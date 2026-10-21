import { describe, it, expect } from "vitest"
import { monthlyCsvLinks, previousMonth, resolveMonth } from "./reports"

describe("month helpers", () => {
  it("rolls December back a year", () => {
    expect(previousMonth("2026-01")).toBe("2025-12")
    expect(previousMonth("2026-10")).toBe("2026-09")
  })

  it("falls back to the current month for junk, future or missing values", () => {
    expect(resolveMonth("2026-09", "2026-10")).toBe("2026-09")
    expect(resolveMonth("2026-13", "2026-10")).toBe("2026-10")
    expect(resolveMonth("2026-11", "2026-10")).toBe("2026-10")
    expect(resolveMonth("abc", "2026-10")).toBe("2026-10")
    expect(resolveMonth(undefined, "2026-10")).toBe("2026-10")
  })

  it("builds month-scoped download links", () => {
    const l = monthlyCsvLinks({ month: "2026-09", date_from: "2026-09-01", date_to: "2026-09-30" })
    expect(l.bills).toBe("/api/proxy/exports/bills.csv?date_from=2026-09-01&date_to=2026-09-30&status=2")
    expect(l.payments).toContain("date_to=2026-09-30")
    expect(l.receipts).toContain("direction=in&counted_only=1")
    expect(l.paymentsOut).toContain("direction=out&counted_only=1")
    expect(l.receivable).toBe("/payments?tab=sales")
    expect(l.summary).toBe("/api/proxy/reports/monthly.csv?month=2026-09")
  })
})
