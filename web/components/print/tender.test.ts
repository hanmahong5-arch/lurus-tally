import { describe, it, expect, beforeEach } from "vitest"
import { saveTender, loadTender, tenderFromPayments } from "./tender"

describe("tender storage", () => {
  beforeEach(() => window.localStorage.clear())

  it("round-trips a record", () => {
    saveTender("b1", { received: "100", change: "5" })
    expect(loadTender("b1")).toEqual({ received: "100", change: "5" })
  })

  it("keeps only the latest 500 records and drops stale ones", () => {
    window.localStorage.setItem("tally-pos-tender:old", JSON.stringify({ received: "1", change: "0", at: Date.now() - 40 * 24 * 3600 * 1000 }))
    for (let i = 0; i < 520; i++) saveTender("b" + i, { received: "1", change: "0" })
    const n = Object.keys(window.localStorage).filter((k) => k.startsWith("tally-pos-tender:")).length
    expect(n).toBeLessThanOrEqual(501)
    expect(loadTender("old")).toBeNull()
    expect(loadTender("b519")).not.toBeNull()
  })
})

describe("tenderFromPayments", () => {
  it("reads 实收 / 找零 from a payment remark", () => {
    expect(tenderFromPayments([{ remark: "实收 100.00，找零 87.70" }])).toEqual({ received: "100.00", change: "87.70" })
  })

  it("ignores payments without a tender remark", () => {
    expect(tenderFromPayments([{ remark: "退货退款 SR-1" }, {}])).toBeNull()
    expect(tenderFromPayments(undefined)).toBeNull()
  })
})
