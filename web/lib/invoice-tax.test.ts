import { describe, it, expect } from "vitest"
import { splitTaxInclusive } from "./invoice-tax"

describe("splitTaxInclusive", () => {
  it("103 at 3% is 100 excluding tax plus 3 tax", () => {
    expect(splitTaxInclusive("103", "0.03")).toEqual({ excl: "100.00", tax: "3.00" })
  })

  it("tax-free keeps the whole total", () => {
    expect(splitTaxInclusive("88.80", "0")).toEqual({ excl: "88.80", tax: "0.00" })
  })

  it("rounds to cents and the parts always add back to the total", () => {
    const r = splitTaxInclusive("100", "0.13")!
    expect(r).toEqual({ excl: "88.50", tax: "11.50" })
    const r2 = splitTaxInclusive("0.05", "0.13")!
    expect((Number(r2.excl) + Number(r2.tax)).toFixed(2)).toBe("0.05")
  })

  it("is null for blank, junk or negative input", () => {
    expect(splitTaxInclusive("", "0.03")).toBeNull()
    expect(splitTaxInclusive("abc", "0.03")).toBeNull()
    expect(splitTaxInclusive("-5", "0.03")).toBeNull()
    expect(splitTaxInclusive("103", "")).toBeNull()
  })
})
