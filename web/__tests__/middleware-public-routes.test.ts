/**
 * middleware.ts route-matcher whitelist tests.
 *
 * Business risk under test: this exact bug already shipped once (S28.1 —
 * `matcher: ["/(dashboard|setup|pos)(.*)"]` never matched /products,
 * /dictionary, /projects because (dashboard) is a Next.js *route group*, not
 * a URL segment — those pages silently ran with no session, then broke on
 * the first client-side fetch). The fix uses a negative-lookahead whitelist
 * instead. This test locks in the two failure directions that whitelist can
 * regress into:
 *   1. A protected page (e.g. /dashboard, /subscription) stops being
 *      intercepted → an unauthenticated visitor reaches a page that expects
 *      a session, and it breaks in a confusing way (or worse, leaks data).
 *   2. /pricing or /login themselves get swept into the "protected" set →
 *      anonymous visitors hit a login wall before ever seeing a price, and
 *      the paid-conversion funnel's entry point silently disappears.
 *
 * We don't import `middleware.ts` directly: it pulls in `@/auth` → `next-auth`,
 * whose beta build has an ESM resolution edge case for `next/server` under
 * Vitest (unrelated to the matcher logic itself). Instead we read the real
 * matcher literal straight out of the source file and feed it through
 * Next.js's own matcher compiler + matcher engine, so the assertions exercise
 * the exact same regex Next.js builds at compile time — not a hand-rolled
 * reimplementation of it.
 */
import { describe, it, expect } from "vitest"
import { readFileSync } from "node:fs"
import path from "node:path"
import { getMiddlewareMatchers } from "next/dist/build/analysis/get-page-static-info"
import { getMiddlewareRouteMatcher } from "next/dist/shared/lib/router/utils/middleware-route-matcher"

function loadMatcherPatternsFromSource(): string[] {
  const src = readFileSync(path.resolve(__dirname, "../middleware.ts"), "utf8")
  const match = src.match(/matcher:\s*\[([^\]]*)\]/)
  if (!match) {
    throw new Error(
      "middleware.ts no longer exports `config.matcher` as a literal array — " +
        "update this test's parser to match the new shape before trusting it.",
    )
  }
  const literals = Array.from(match[1].matchAll(/["'`]([^"'`]+)["'`]/g)).map((m) => m[1])
  if (literals.length === 0) {
    throw new Error("matcher array found but contained no string literals")
  }
  return literals
}

function buildIsIntercepted(): (pathname: string) => boolean {
  const patterns = loadMatcherPatternsFromSource()
  const matchers = getMiddlewareMatchers(patterns, {})
  const match = getMiddlewareRouteMatcher(matchers)
  // Our matcher has no `has`/`missing` clauses, so `match()` never reads the
  // request/query args at runtime — safe to erase them from this helper's
  // test-facing signature rather than fabricate a fake BaseNextRequest.
  return (pathname: string) => match(pathname, {} as never, {})
}

describe("middleware.ts matcher — public vs protected routes", () => {
  it("/pricing must stay public: potential customers reach pricing without a login wall", () => {
    const isIntercepted = buildIsIntercepted()
    expect(isIntercepted("/pricing")).toBe(false)
  })

  it("/login must stay public: an unauthenticated visitor needs to reach the sign-in page itself", () => {
    const isIntercepted = buildIsIntercepted()
    expect(isIntercepted("/login")).toBe(false)
  })

  it("/api/* and Next.js internals stay public: they carry their own auth check, and gating them would break the OIDC callback + proxy route + static assets", () => {
    const isIntercepted = buildIsIntercepted()
    expect(isIntercepted("/api/proxy/v1/billing/subscribe")).toBe(false)
    expect(isIntercepted("/api/auth/callback/oidc")).toBe(false)
    expect(isIntercepted("/_next/static/chunk.js")).toBe(false)
    expect(isIntercepted("/favicon.ico")).toBe(false)
  })

  it("real app pages under a route group are still gated — regression guard for the S28.1 bug (route group name treated as a URL segment)", () => {
    const isIntercepted = buildIsIntercepted()
    // (dashboard) is a route group: these URLs have no "/dashboard" prefix.
    expect(isIntercepted("/products")).toBe(true)
    expect(isIntercepted("/dictionary")).toBe(true)
    expect(isIntercepted("/projects")).toBe(true)
    expect(isIntercepted("/subscription")).toBe(true)
    expect(isIntercepted("/setup")).toBe(true)
  })

  it("KNOWN GAP (documented, not a pass/fail guard on 'good' behaviour): the negative lookahead is a literal prefix match, not a path-segment boundary — any future route whose first segment starts with the literal text 'login'/'pricing'/'api'/'_next'/'favicon.ico' silently becomes public. No such route exists today, but adding e.g. /pricing-experiment or /apiaries later would ship it unauthenticated with zero test failures elsewhere.", () => {
    const isIntercepted = buildIsIntercepted()
    expect(isIntercepted("/pricing-internal-admin")).toBe(false)
    expect(isIntercepted("/apiaries")).toBe(false)
  })
})
