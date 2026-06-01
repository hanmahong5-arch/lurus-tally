import { describe, it, expect, vi, beforeEach } from "vitest"
import { renderHook } from "@testing-library/react"

// Control useSession per-test. This file-level mock overrides the global stub
// in vitest.setup.ts for this module only.
const useSessionMock = vi.fn()
vi.mock("next-auth/react", () => ({ useSession: () => useSessionMock() }))

import { useTenantId } from "@/hooks/use-tenant-id"

describe("useTenantId", () => {
  beforeEach(() => {
    useSessionMock.mockReset()
  })

  it("returns the tenant id when the session carries one", () => {
    useSessionMock.mockReturnValue({ data: { user: { tenantId: "tenant-abc" } } })
    const { result } = renderHook(() => useTenantId())
    expect(result.current).toBe("tenant-abc")
  })

  it("returns undefined when the session is still loading (no data)", () => {
    useSessionMock.mockReturnValue({ data: undefined, status: "loading" })
    const { result } = renderHook(() => useTenantId())
    expect(result.current).toBeUndefined()
  })

  it("returns undefined when there is no session", () => {
    useSessionMock.mockReturnValue({ data: null, status: "unauthenticated" })
    const { result } = renderHook(() => useTenantId())
    expect(result.current).toBeUndefined()
  })

  it("returns undefined when the session user has a null tenant", () => {
    useSessionMock.mockReturnValue({ data: { user: { tenantId: null } } })
    const { result } = renderHook(() => useTenantId())
    // Never falls back to any literal id — null tenant means "unknown", not dev.
    expect(result.current).toBeUndefined()
  })
})
