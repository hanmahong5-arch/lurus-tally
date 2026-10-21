import { describe, it, expect } from "vitest"
import { readFileSync } from "node:fs"
import path from "node:path"

import { ROLE_PERMS, profileTypeLabel, roleCan, roleLabel, roleLabelFor, type ShopRole } from "./roles"

// The Go test writes this file from the backend permission table; both sides
// read the same copy, so the two cannot drift apart.
const fixture: Record<string, string[]> = JSON.parse(
  readFileSync(
    path.resolve(__dirname, "../../internal/domain/permission/testdata/matrix.json"),
    "utf-8",
  ),
)

describe("role matrix matches the backend", () => {
  const roles: ShopRole[] = ["owner", "manager", "clerk", "cashier", "finance"]

  for (const role of roles) {
    it(`${role} holds exactly the backend permissions`, () => {
      expect([...ROLE_PERMS[role]].sort()).toEqual([...fixture[role]].sort())
    })
  }

  it("covers the same roles", () => {
    expect(Object.keys(fixture).sort()).toEqual([...roles].sort())
  })
})

describe("roleCan on the new permissions", () => {
  it("lets the cashier check out but not draft", () => {
    expect(roleCan("cashier", "bill.checkout")).toBe(true)
    expect(roleCan("cashier", "bill.draft")).toBe(false)
    expect(roleCan("cashier", "bill.return")).toBe(false)
    expect(roleCan("cashier", "bill.return.own")).toBe(true)
  })
  it("keeps the clerk away from cost, approval and billing", () => {
    expect(roleCan("clerk", "cost.view")).toBe(false)
    expect(roleCan("clerk", "bill.approve")).toBe(false)
    expect(roleCan("clerk", "purchase.view")).toBe(true)
    expect(roleCan("clerk", "product.write")).toBe(true)
  })
  it("is false for an unknown role", () => {
    expect(roleCan(null, "bill.read")).toBe(false)
  })
})

describe("roleLabel", () => {
  it("names every role in Chinese", () => {
    expect(roleLabel("owner")).toBe("老板")
    expect(roleLabel("cashier")).toBe("收银")
  })
})

describe("display labels", () => {
  it("names the business type in Chinese, never the raw value", () => {
    expect(profileTypeLabel("cross_border")).toBe("跨境电商")
    expect(profileTypeLabel("retail")).toBe("零售")
    expect(profileTypeLabel("")).toBe("未设置")
    expect(profileTypeLabel("something_new")).toBe("其他")
  })
  it("folds legacy and owner-flag roles before naming them", () => {
    expect(roleLabelFor("admin", false)).toBe("店长")
    expect(roleLabelFor("clerk", true)).toBe("老板")
    expect(roleLabelFor(null, false)).toBe("—")
  })
})
