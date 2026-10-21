import { describe, it, expect, beforeEach } from "vitest"
import { saveBillCopy, takeBillCopy } from "./bill-copy"

describe("bill-copy", () => {
  beforeEach(() => sessionStorage.clear())

  it("round-trips once and then clears", () => {
    saveBillCopy("sale", {
      partner_id: "c1",
      items: [{ product_id: "p1", qty: "2", unit_price: "9.80" }],
    })
    expect(takeBillCopy("purchase")).toBeNull()
    const got = takeBillCopy("sale")
    expect(got?.partner_id).toBe("c1")
    expect(got?.items).toHaveLength(1)
    expect(takeBillCopy("sale")).toBeNull()
  })

  it("ignores garbage", () => {
    sessionStorage.setItem("tally:copy-bill:sale", "{not json")
    expect(takeBillCopy("sale")).toBeNull()
  })
})
