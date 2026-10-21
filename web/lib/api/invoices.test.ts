import { describe, it, expect } from "vitest"
import { invoiceTotal, remainingToInvoice, sumMoney, moneyGt } from "./invoices"

describe("invoice money helpers", () => {
  it("adds excl + tax without float drift", () => {
    expect(invoiceTotal("0.1", "0.2")).toBe("0.30")
    expect(invoiceTotal("100", "")).toBe("100.00")
    expect(invoiceTotal("", "5")).toBe("")
    expect(invoiceTotal("abc", "5")).toBe("")
  })

  it("defaults the allocation to what is still uninvoiced, never negative", () => {
    expect(remainingToInvoice("200.0000", "50.0000")).toBe("150.00")
    expect(remainingToInvoice("200.0000", undefined)).toBe("200.00")
    expect(remainingToInvoice("100.0000", "120.0000")).toBe("0.00")
    expect(remainingToInvoice("100.0050", "0")).toBe("100.00")
  })

  it("sums and compares decimals exactly", () => {
    expect(sumMoney(["0.1", "0.2", "0.3"])).toBe("0.60")
    expect(moneyGt("0.30", "0.3")).toBe(false)
    expect(moneyGt("0.31", "0.3")).toBe(true)
  })
})
