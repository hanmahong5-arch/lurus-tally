/**
 * Pricing → login → post-login-return round trip.
 *
 * Business risk under test: a real incident class where the "subscribe"
 * button sends the visitor to /login?<PARAM>=<page>, the middleware's own
 * redirect-to-login uses a DIFFERENT param name, and/or the login page reads
 * yet another name back out of the URL. Any mismatch means the user signs in
 * successfully and lands on /dashboard instead of back on /subscription —
 * the paid-conversion funnel's last hop silently drops them, and nothing
 * throws an error anywhere (it's just the wrong page).
 *
 * This test does not hardcode the param name as a fixed literal. It extracts
 * the name each of the three call sites actually uses from source, so it
 * fails if any ONE of them drifts from the other two — which is exactly the
 * failure mode of the incident (a rename in one place, missed in another).
 */
import { describe, it, expect, vi } from "vitest"
import { readFileSync } from "node:fs"
import path from "node:path"
import { render, screen } from "@testing-library/react"

vi.mock("next/link", () => ({
  default: ({
    href,
    children,
    ...props
  }: {
    href: string
    children: React.ReactNode
    [key: string]: unknown
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))

import PricingPage from "@/app/pricing/page"

function readSrc(relPath: string): string {
  return readFileSync(path.resolve(__dirname, "..", relPath), "utf8")
}

/** The param name middleware.ts attaches when bouncing an anonymous visitor to /login. */
function middlewareRedirectParam(): string {
  const src = readSrc("middleware.ts")
  const m = src.match(/loginUrl\.searchParams\.set\(\s*["'`]([^"'`]+)["'`]/)
  if (!m) throw new Error("middleware.ts no longer sets a searchParam on the /login redirect")
  return m[1]
}

/** The param name the login page itself reads back out of its `searchParams` prop. */
function loginPageReadParam(): string {
  const src = readSrc("app/(auth)/login/page.tsx")
  const m = src.match(/searchParams\?\.(\w+)\s*\?\?/)
  if (!m) throw new Error("login/page.tsx no longer reads a fallback-able searchParams field")
  return m[1]
}

describe("pricing → login → post-login-return param name consistency", () => {
  it("middleware's redirect-to-login param matches what the login page reads back", () => {
    expect(middlewareRedirectParam()).toBe(loginPageReadParam())
  })

  it("the pricing page's subscribe CTA sends the visitor to /login using that SAME param name (not e.g. ?next=)", () => {
    const expectedParam = loginPageReadParam()

    render(<PricingPage />)
    // Every paid plan's CTA reads "登录后订阅" — check all of them, not just one.
    const subscribeLinks = screen.getAllByRole("link", { name: "登录后订阅" })
    expect(subscribeLinks.length).toBeGreaterThan(0)

    for (const subscribeLink of subscribeLinks) {
      const href = subscribeLink.getAttribute("href")
      expect(href).not.toBeNull()

      const url = new URL(href!, "https://tally.example")
      expect(url.pathname).toBe("/login")
      expect(url.searchParams.get(expectedParam)).toBe("/subscription")
      // Guard the literal historical incident: a stray `?next=` alongside/instead
      // of the real param would silently be ignored by the login page.
      expect(url.searchParams.has("next")).toBe(false)
    }
  })
})
