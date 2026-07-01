import { auth } from "@/auth"
import { NextResponse } from "next/server"

// Middleware applies to all app routes except: /login (the sign-in page itself),
// /pricing (the public, anonymous-reachable plans page — gateway to self-service
// signup; must NOT redirect to login), /api/* (NextAuth handlers + the proxy
// route which has its own session check), and Next.js internals/static. Using a
// negative matcher avoids the "(dashboard) is a route group, not a URL segment"
// bug where /products, /dictionary, /projects etc. silently bypassed auth.
export const config = {
  matcher: ["/((?!login|pricing|api|_next|favicon.ico).*)"],
}

export default auth((req) => {
  const { nextUrl, auth: session } = req as typeof req & { auth: typeof req.auth }

  const isAuthenticated = !!session

  // Not authenticated → redirect to login.
  if (!isAuthenticated) {
    const loginUrl = new URL("/login", nextUrl.origin)
    loginUrl.searchParams.set("callbackUrl", nextUrl.pathname)
    return NextResponse.redirect(loginUrl)
  }

  const profileType = (session as { user?: { profileType?: string | null } })?.user?.profileType
  const isOnSetup = nextUrl.pathname.startsWith("/setup")
  const isOnPos = nextUrl.pathname.startsWith("/pos")

  // Authenticated but no profile (first-time user) and not already on setup → redirect to setup.
  if (!profileType && !isOnSetup) {
    return NextResponse.redirect(new URL("/setup", nextUrl.origin))
  }

  // POS is retail-only. Cross-border / hybrid profiles are redirected.
  // retail profile or null (first-time / unknown) is allowed through;
  // the layout.tsx server component does the definitive check.
  if (isOnPos && profileType && profileType !== "retail") {
    const res = NextResponse.redirect(new URL("/dashboard", nextUrl.origin))
    // Flash message — consumed once by ToastProvider on the client and cleared.
    res.cookies.set("tally-flash", JSON.stringify({
      level: "warning",
      text: "POS 收银台仅对零售业态开放",
    }), { path: "/", maxAge: 30, sameSite: "lax" })
    return res
  }

  return NextResponse.next()
})
