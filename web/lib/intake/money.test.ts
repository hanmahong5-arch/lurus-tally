import { describe, expect, it } from "vitest"
import { comparedTotal, moneyGap, moneyReconciles } from "./money"

describe("moneyReconciles", () => {
  it("accepts a three-way sum", () => {
    expect(moneyReconciles("100.00", "13.00", "113.00")).toBe(true)
    expect(moneyReconciles("100", "13", "113")).toBe(true)
  })

  it("rejects a mismatch, a blank, and a lone total", () => {
    expect(moneyReconciles("100.00", "13.00", "112.00")).toBe(false)
    expect(moneyReconciles("", "13.00", "113.00")).toBe(false)
    expect(moneyReconciles("", "", "113.00")).toBe(false)
  })

  it("states the amount gap against a bill", () => {
    expect(moneyGap("113.00", "100.00")).toBe("比这张单多 13.00")
    expect(moneyGap("100.00", "113.00")).toBe("比这张单少 13.00")
    expect(moneyGap("113.00", "113.0000")).toBe("与这张单一致")
    expect(comparedTotal("113.00", ["100.00", "13.00"])).toBe("与勾选单据合计一致")
    expect(comparedTotal("113.00", [])).toBe("")
  })
})
