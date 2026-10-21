import { describe, it, expect, vi, beforeEach } from "vitest"
import { renderHook } from "@testing-library/react"

let sessionUser: { role?: string | null; isOwner?: boolean } | null = null
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: sessionUser ? { user: sessionUser } : null }),
}))

import { normalizeRole, roleCan, roleLabel, useRole } from "./use-role"

describe("normalizeRole", () => {
  it("keeps the five shop roles", () => {
    for (const r of ["owner", "manager", "clerk", "cashier", "finance"]) {
      expect(normalizeRole(r, false)).toBe(r)
    }
  })
  it("folds legacy values and the owner flag", () => {
    expect(normalizeRole("admin", false)).toBe("manager")
    expect(normalizeRole("viewer", false)).toBe("clerk")
    expect(normalizeRole("clerk", true)).toBe("owner")
  })
  it("returns null when the role is not known yet", () => {
    expect(normalizeRole(null, false)).toBeNull()
    expect(normalizeRole("", false)).toBeNull()
  })
  it("gives an unknown value the least privilege, never owner", () => {
    expect(normalizeRole("weird", false)).toBe("clerk")
  })
})

describe("roleCan", () => {
  it("lets only the owner manage billing", () => {
    expect(roleCan("owner", "billing.manage")).toBe(true)
    for (const r of ["manager", "clerk", "cashier", "finance"] as const) {
      expect(roleCan(r, "billing.manage")).toBe(false)
    }
  })
  it("lets owner, manager and finance see cost", () => {
    expect(roleCan("owner", "cost.view")).toBe(true)
    expect(roleCan("manager", "cost.view")).toBe(true)
    expect(roleCan("finance", "cost.view")).toBe(true)
    expect(roleCan("clerk", "cost.view")).toBe(false)
    expect(roleCan("cashier", "cost.view")).toBe(false)
  })
  it("lets only owner and manager manage the team", () => {
    expect(roleCan("owner", "team.manage")).toBe(true)
    expect(roleCan("manager", "team.manage")).toBe(true)
    expect(roleCan("finance", "team.manage")).toBe(false)
  })
  it("denies everything while the role is unknown", () => {
    expect(roleCan(null, "cost.view")).toBe(false)
  })
})

describe("roleLabel", () => {
  it("speaks shop language", () => {
    expect(roleLabel("owner")).toBe("老板")
    expect(roleLabel("manager")).toBe("店长")
    expect(roleLabel("clerk")).toBe("店员")
    expect(roleLabel("cashier")).toBe("收银")
    expect(roleLabel("finance")).toBe("财务")
  })
})

describe("useRole", () => {
  beforeEach(() => {
    sessionUser = null
  })
  it("reports unknown while there is no session", () => {
    const { result } = renderHook(() => useRole())
    expect(result.current.role).toBeNull()
    expect(result.current.known).toBe(false)
    expect(result.current.can("cost.view")).toBe(false)
  })
  it("derives permissions from the session role", () => {
    sessionUser = { role: "finance", isOwner: false }
    const { result } = renderHook(() => useRole())
    expect(result.current.role).toBe("finance")
    expect(result.current.known).toBe(true)
    expect(result.current.can("cost.view")).toBe(true)
    expect(result.current.can("billing.manage")).toBe(false)
  })
})

describe("bill permissions", () => {
  it("lets only owner and manager approve, cancel and return", () => {
    for (const perm of ["bill.approve", "bill.cancel", "bill.return"] as const) {
      expect(roleCan("owner", perm)).toBe(true)
      expect(roleCan("manager", perm)).toBe(true)
      for (const r of ["clerk", "cashier", "finance"] as const) {
        expect(roleCan(r, perm)).toBe(false)
      }
    }
  })
  it("lets the clerk draft but not the cashier or finance", () => {
    expect(roleCan("clerk", "bill.draft")).toBe(true)
    expect(roleCan("cashier", "bill.draft")).toBe(false)
    expect(roleCan("finance", "bill.draft")).toBe(false)
  })
})
