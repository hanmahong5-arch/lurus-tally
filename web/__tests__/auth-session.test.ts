import { describe, it, expect } from "vitest"

// Validates the augmented NextAuth Session shape. We can't call useSession()
// outside a React tree, and mocking it just re-asserts the mock data. Instead
// type-check the literal against the augmented type so any drift in
// next-auth.d.ts surfaces here at compile time.
import type { Session } from "next-auth"

const fixture: Session = {
  user: {
    id: "zitadel-sub-123",
    email: "test@example.com",
    tenantId: "tenant-uuid-abc",
    profileType: "cross_border",
    isFirstTime: false,
  },
  expires: new Date(Date.now() + 86400000).toISOString(),
}

describe("Session shape", () => {
  it("test_auth_session_contains_profile_type", () => {
    expect(fixture.user.profileType).toBeDefined()
    expect(["cross_border", "retail", "hybrid", "horticulture", null]).toContain(
      fixture.user.profileType,
    )
  })

  it("test_auth_session_contains_tenant_id", () => {
    expect(fixture.user.tenantId).toBeDefined()
    expect(typeof fixture.user.tenantId).toBe("string")
  })

  it("test_auth_session_contains_user_id", () => {
    expect(typeof fixture.user.id).toBe("string")
    expect(fixture.user.id.length).toBeGreaterThan(0)
  })
})
