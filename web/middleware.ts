import { auth } from "@/auth"
import { NextResponse } from "next/server"
import { normalizeRole } from "@/lib/roles"

// Middleware applies to all app routes except: /login (the sign-in page itself),
// /pricing (public marketing funnel — must be reachable by anonymous visitors
// so the paid-conversion entry point is not gated; pricing/page.tsx is a pure
// server component with no session dependency), /api/* (NextAuth handlers + the
// proxy route which has its own session check), and Next.js internals/static.
// Using a negative matcher avoids the "(dashboard) is a route group, not a URL
// segment" bug where /products, /dictionary, /projects etc. silently bypassed auth.
export const config = {
  matcher: ["/((?!login|pricing|api|_next|favicon.ico|manifest.webmanifest|sw.js|swe-worker|offline|icons|zxing).*)"],
}

export default auth((req) => {
  const { nextUrl, auth: session } = req as typeof req & { auth: typeof req.auth }

  const isAuthenticated = !!session

  // Not authenticated → redirect to login.
  if (!isAuthenticated) {
    const loginUrl = new URL("/login", nextUrl.origin)
    // An invite link carries its token in the query; keep it through sign-in.
    const keepQuery = nextUrl.pathname === "/join"
    loginUrl.searchParams.set("callbackUrl", keepQuery ? nextUrl.pathname + nextUrl.search : nextUrl.pathname)
    return NextResponse.redirect(loginUrl)
  }

  const profileType = (session as { user?: { profileType?: string | null } })?.user?.profileType
  const isOnSetup = nextUrl.pathname.startsWith("/setup")
  // A person with an invite link has no shop yet; /join is how they get one.
  const isOnJoin = nextUrl.pathname === "/join"
  const isOnPos = nextUrl.pathname.startsWith("/pos")

  // Authenticated but no profile (first-time user) and not already on setup → redirect to setup.
  if (!profileType && !isOnSetup && !isOnJoin) {
    return NextResponse.redirect(new URL("/setup", nextUrl.origin))
  }

  // A cashier works at the till: the back-office home sends them to the POS. Only retail shops
  // have a POS (the rule below would bounce them straight back), so other shops stay put.
  const user = (session as { user?: { role?: string | null; isOwner?: boolean } })?.user
  const isCashier = normalizeRole(user?.role, user?.isOwner === true) === "cashier"
  const isOnHome = nextUrl.pathname === "/" || nextUrl.pathname === "/dashboard" || nextUrl.pathname.startsWith("/dashboard/")
  if (isCashier && profileType === "retail" && isOnHome) {
    return NextResponse.redirect(new URL("/pos", nextUrl.origin))
  }

  // POS is retail-only. Cross-border / hybrid profiles are redirected.
  // retail profile or null (first-time / unknown) is allowed through;
  // the layout.tsx server component does the definitive check.
  if (isOnPos && profileType && profileType !== "retail") {
    const res = NextResponse.redirect(new URL("/dashboard", nextUrl.origin))
    // Flash message — consumed once by ToastProvider on the client and cleared.
    res.cookies.set("tally-flash", JSON.stringify({
      level: "warning",
      text: "收银台只在零售店开放",
    }), { path: "/", maxAge: 30, sameSite: "lax" })
    return res
  }

  return NextResponse.next()
})
