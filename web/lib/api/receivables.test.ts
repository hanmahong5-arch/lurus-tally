import { describe, expect, it } from "vitest"
import { openBillsHref, unpaidByPartner, type ReceivablesResult } from "./receivables"

const row = (id: string | null, unpaid: string) => ({
  partner_id: id,
  partner_name: "x",
  unpaid,
  b0_30: unpaid,
  b31_60: "0.00",
  b61_90: "0.00",
  b90p: "0.00",
  oldest_bill_date: "2026-10-01",
})

describe("receivables helpers", () => {
  it("maps unpaid by partner and skips walk-in rows", () => {
    const r: ReceivablesResult = {
      direction: "ar",
      as_of: "2026-10-10",
      items: [row("a", "10.50"), row(null, "3.00")],
      total: row(null, "13.50"),
    }
    const m = unpaidByPartner(r)
    expect(m.get("a")).toBe("10.50")
    expect(m.size).toBe(1)
  })

  it("builds the drill-down link without ever exposing the id as text", () => {
    expect(openBillsHref("ar", "p1", "老王")).toBe("/payments?partner=p1&pname=%E8%80%81%E7%8E%8B")
    expect(openBillsHref("ap", "p1", "供应商")).toContain("tab=purchases")
  })
})
