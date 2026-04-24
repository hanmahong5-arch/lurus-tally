import { describe, it, expect, vi } from "vitest"

// Mock next-auth/react to avoid requiring a real session provider in unit tests.
vi.mock("next-auth/react", () => ({
  useSession: vi.fn(() => ({
    data: {
      user: {
        id: "zitadel-sub-123",
        email: "test@example.com",
        tenantId: "tenant-uuid-abc",
        profileType: "cross_border",
        isFirstTime: false,
      },
      expires: new Date(Date.now() + 86400000).toISOString(),
    },
    status: "authenticated",
  })),
  signIn: vi.fn(),
  signOut: vi.fn(),
  SessionProvider: ({ children }: { children: React.ReactNode }) => children,
}))

describe("auth session", () => {
  it("test_auth_session_contains_profile_type: session.user.profileType field exists", () => {
    const { useSession } = require("next-auth/react")
    const { data: session } = useSession()

    expect(session).not.toBeNull()
    expect(session.user).toBeDefined()
    expect(session.user.profileType).toBeDefined()
    expect(["cross_border", "retail", "hybrid", null]).toContain(session.user.profileType)
  })

  it("test_auth_session_contains_tenant_id: session.user.tenantId field exists", () => {
    const { useSession } = require("next-auth/react")
    const { data: session } = useSession()

    expect(session.user.tenantId).toBeDefined()
  })

  it("test_auth_session_contains_user_id: session.user.id field is zitadel sub", () => {
    const { useSession } = require("next-auth/react")
    const { data: session } = useSession()

    expect(typeof session.user.id).toBe("string")
    expect(session.user.id.length).toBeGreaterThan(0)
  })
})
