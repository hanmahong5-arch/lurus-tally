import { describe, it, expect, vi } from "vitest"
import { NextRequest } from "next/server"

// The wrapper only hands the request and its session to the handler.
vi.mock("@/auth", () => ({
  auth: (handler: (req: unknown) => unknown) => handler,
}))

import middleware from "./middleware"

type Session = { user: { profileType: string | null; role?: string | null; isOwner?: boolean } } | null

function run(path: string, session: Session) {
  const req = new NextRequest(new URL(path, "http://localhost:3001"))
  ;(req as unknown as { auth: Session }).auth = session
  return (middleware as unknown as (r: NextRequest) => Response)(req)
}

describe("middleware till gate", () => {
  it("flashes the retail-only wording when a non-retail shop opens the till", () => {
    const res = run("/pos", { user: { profileType: "cross_border", role: "owner" } })
    expect(res.headers.get("location")).toBe("http://localhost:3001/dashboard")
    const flash = (res as unknown as { cookies: { get: (n: string) => { value: string } | undefined } }).cookies.get("tally-flash")?.value ?? ""
    expect(decodeURIComponent(flash)).toContain("收银台只在零售店开放")
  })
})

describe("middleware cashier landing", () => {
  it("sends a cashier of a retail shop from the dashboard to the till", () => {
    const res = run("/dashboard", { user: { profileType: "retail", role: "cashier" } })
    expect(res.headers.get("location")).toBe("http://localhost:3001/pos")
  })

  it("keeps a cashier in a shop without a till where they are", () => {
    const res = run("/dashboard", { user: { profileType: "cross_border", role: "cashier" } })
    expect(res.headers.get("location")).toBeNull()
  })

  it("lets a cashier stay on the till", () => {
    const res = run("/pos", { user: { profileType: "retail", role: "cashier" } })
    expect(res.headers.get("location")).toBeNull()
  })

  it("leaves a manager on the dashboard", () => {
    const res = run("/dashboard", { user: { profileType: "retail", role: "manager" } })
    expect(res.headers.get("location")).toBeNull()
  })

  it("never treats the owner flag as a cashier", () => {
    const res = run("/dashboard", { user: { profileType: "retail", role: "cashier", isOwner: true } })
    expect(res.headers.get("location")).toBeNull()
  })
})
