import { auth } from "@/auth"
import { NextResponse } from "next/server"

// Middleware applies to dashboard, setup, and POS routes.
export const config = {
  matcher: ["/(dashboard|setup|pos)(.*)"],
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
    return NextResponse.redirect(
      new URL("/dashboard?error=pos-retail-only", nextUrl.origin)
    )
  }

  return NextResponse.next()
})
