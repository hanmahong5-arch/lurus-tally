/**
 * middleware.ts invite-link behaviour. The auth() wrapper is replaced by an
 * identity function so the handler can be called with a fake request.
 */
import { describe, it, expect, vi } from "vitest"

vi.mock("@/auth", () => ({ auth: (handler: unknown) => handler }))

import middleware from "../middleware"

type Handler = (req: unknown) => Response | undefined

function request(pathname: string, search: string, session: unknown) {
  const url = new URL(`http://localhost:3001${pathname}${search}`)
  return { auth: session, nextUrl: url }
}

const run = middleware as unknown as Handler

describe("middleware /join", () => {
  it("keeps the invite token through the sign-in redirect", () => {
    const res = run(request("/join", "?token=abc123", null))
    const location = new URL(res!.headers.get("location")!)
    expect(location.pathname).toBe("/login")
    expect(location.searchParams.get("callbackUrl")).toBe("/join?token=abc123")
  })

  it("drops the query for other pages", () => {
    const res = run(request("/products", "?q=x", null))
    const location = new URL(res!.headers.get("location")!)
    expect(location.searchParams.get("callbackUrl")).toBe("/products")
  })

  it("does not send a signed-in person without a shop from /join to /setup", () => {
    const res = run(request("/join", "?token=abc123", { user: { profileType: null } }))
    const location = res?.headers.get("location")
    expect(location ?? "").not.toContain("/setup")
  })

  it("still sends a signed-in person without a shop from other pages to /setup", () => {
    const res = run(request("/products", "", { user: { profileType: null } }))
    expect(new URL(res!.headers.get("location")!).pathname).toBe("/setup")
  })
})
