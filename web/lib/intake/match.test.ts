import { describe, expect, it } from "vitest"
import { coverNote, suggestBillIds } from "./match"

const bill = (id: string, remaining: string) => ({ id, status: 2, remaining })

describe("suggestBillIds", () => {
  it("picks one exact bill, else a pair, else what fits under the total", () => {
    expect(suggestBillIds("113.00", [bill("a", "40.00"), bill("b", "113.00"), bill("c", "73.00")])).toEqual(["b"])
    expect(suggestBillIds("113.00", [bill("a", "40.00"), bill("c", "73.00"), bill("d", "10.00")])).toEqual(["a", "c"])
    expect(suggestBillIds("100.00", [bill("a", "80.00"), bill("b", "50.00"), bill("c", "20.00")])).toEqual(["a", "c"])
    expect(suggestBillIds("113.00", [{ id: "draft", status: 0, remaining: "113.00" }])).toEqual([])
  })

  it("says what is left over", () => {
    expect(coverNote("113.00", ["113.00"])).toBe("勾选的未开票金额和这张发票一致")
    expect(coverNote("113.00", ["100.00"])).toBe("还有 13.00 元没有单据可对")
    expect(coverNote("113.00", ["200.00"])).toBe("多出的 87.00 元留在单据上，不会整笔记到这张发票")
  })
})