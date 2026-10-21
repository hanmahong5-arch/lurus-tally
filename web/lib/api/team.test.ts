import { describe, it, expect, beforeEach, afterEach, vi } from "vitest"
import {
  listMembers,
  updateMemberRole,
  removeMember,
  createInvite,
  listInvites,
  revokeInvite,
  joinShop,
  ASSIGNABLE_ROLES,
} from "./team"

// The proxy prepends /api/v1 itself; a path that already carries it ends up at
// /api/v1/api/v1/team/... and 404s. Lock every team call to /api/proxy/team/...
const mockFetch = vi.fn()

beforeEach(() => {
  mockFetch.mockReset()
  mockFetch.mockImplementation(() =>
    Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ items: [] }) }),
  )
  vi.stubGlobal("fetch", mockFetch)
})

afterEach(() => {
  vi.restoreAllMocks()
})

function calledUrl(): string {
  return String(mockFetch.mock.calls[0][0])
}

describe("team api paths", () => {
  it.each([
    ["listMembers", () => listMembers(), "/api/proxy/team/members"],
    ["updateMemberRole", () => updateMemberRole(7, "clerk"), "/api/proxy/team/members/7"],
    ["removeMember", () => removeMember(7), "/api/proxy/team/members/7"],
    ["createInvite", () => createInvite("cashier", 72), "/api/proxy/team/invites"],
    ["listInvites", () => listInvites(), "/api/proxy/team/invites"],
    ["revokeInvite", () => revokeInvite("inv-1"), "/api/proxy/team/invites/inv-1"],
    ["joinShop", () => joinShop("tok"), "/api/proxy/team/join"],
  ])("%s hits the proxy without a doubled prefix", async (_name, call, want) => {
    await call()
    expect(calledUrl()).toBe(want)
  })
})

describe("ASSIGNABLE_ROLES hints", () => {
  it("describes manager and cashier the way the shop works", () => {
    const hint = (v: string) => ASSIGNABLE_ROLES.find((r) => r.value === v)?.hint
    expect(hint("manager")).toBe("能审核、看成本、管理店员收银财务，不能管付费")
    expect(hint("cashier")).toBe("收银结账、查价，可退当天自己开的小票")
  })
})
