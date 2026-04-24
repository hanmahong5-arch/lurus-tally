import { auth } from "@/auth"
import { NextResponse } from "next/server"

// Middleware applies to all dashboard and setup routes.
export const config = {
  matcher: ["/(dashboard|setup)(.*)"],
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

  // Authenticated but no profile (first-time user) and not already on setup → redirect to setup.
  if (!profileType && !isOnSetup) {
    return NextResponse.redirect(new URL("/setup", nextUrl.origin))
  }

  return NextResponse.next()
})
