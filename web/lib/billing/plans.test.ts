import { describe, it, expect } from "vitest"
import { TALLY_PLANS } from "./plans"

// 套餐卖点只写系统实际执行的限制；商品数、仓库数、人数上限目前都没有执行。
describe("TALLY_PLANS copy", () => {
  const text = JSON.stringify(TALLY_PLANS.map((p) => ({ n: p.name, t: p.tagline, f: p.features })))

  it("keeps the plan codes and cycles in step with the seed migration", () => {
    expect(TALLY_PLANS.map((p) => [p.code, p.cycle])).toEqual([
      ["free", "forever"],
      ["pro", "monthly"],
      ["pro_yearly", "yearly"],
      ["enterprise", "monthly"],
      ["enterprise_yearly", "yearly"],
    ])
  })

  it("does not promise the assistant or any limit the system does not enforce", () => {
    expect(text).not.toMatch(/AI|智能助手|自然语言|SKU|单用户|\d+ ?用户|\d+ ?仓库|\d+ ?种商品|不限商品|不限.{0,6}用户|仓库数|上限/)
  })
})
