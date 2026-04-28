import NextAuth from "next-auth"
import Zitadel from "next-auth/providers/zitadel"

declare module "next-auth" {
  interface Session {
    accessToken?: string
    user: {
      id: string
      email?: string | null
      name?: string | null
      image?: string | null
      tenantId: string | null
      profileType: string | null
      isFirstTime: boolean
      role?: string | null
      isOwner?: boolean
    }
  }
}

// Note: NextAuth v5 types JWT as Record<string, unknown>; augmenting
// "next-auth/jwt" subpath causes "module not found" under some module
// resolution configurations. We rely on local casts inside the jwt callback
// instead.

// BACKEND_URL is the in-cluster service URL for tally-backend.
// Falls back to the public domain only when running outside K8s.
const BACKEND_URL = process.env.BACKEND_URL ?? "http://tally-backend:18200"

// Profile cache TTL — re-fetch /api/v1/me at most once every 60s on session
// access. We invalidate eagerly via NextAuth update() after /setup submit.
const PROFILE_TTL_MS = 60_000

interface MePayload {
  user_id: string
  tenant_id: string
  email: string
  display_name: string
  role: string
  is_owner: boolean
  profile_type: string
  is_first_time: boolean
}

// fetchMe calls the backend /api/v1/me with the user's access_token. On any
// failure it returns null and the caller treats the user as first-time so the
// frontend redirects to /setup. This is fail-safe by design — a flaky backend
// or expired token never leaks the user into a half-hydrated dashboard.
async function fetchMe(accessToken: string): Promise<MePayload | null> {
  try {
    const res = await fetch(`${BACKEND_URL}/api/v1/me`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      // server-side fetch in NextAuth callback — no caching
      cache: "no-store",
    })
    if (!res.ok) return null
    return (await res.json()) as MePayload
  } catch {
    return null
  }
}

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Zitadel({
      clientId: process.env.ZITADEL_CLIENT_ID!,
      issuer: process.env.ZITADEL_ISSUER ?? "https://auth.lurus.cn",
      // PKCE is enabled by default for Zitadel provider.
    }),
  ],
  pages: {
    signIn: "/login",
  },
  callbacks: {
    // jwt is invoked once per request after sign-in, and additionally on
    // explicit update() calls from the client. We use this hook to:
    //   1. capture the access_token from the OIDC `account` (only present at sign-in)
    //   2. lazily fetch /api/v1/me to populate tenant/profile fields on the token
    //   3. refresh on explicit update() (post /setup submit)
    async jwt({ token, account, profile, trigger }) {
      const t = token as Record<string, unknown>

      // First sign-in: capture sub + access_token from the OIDC account.
      if (account && profile) {
        if (typeof profile.sub === "string") {
          t.sub = profile.sub
        }
        if (typeof account.access_token === "string") {
          t.accessToken = account.access_token
        }
      }

      const accessToken = typeof t.accessToken === "string" ? t.accessToken : ""
      const fetchedAt = typeof t.profileFetchedAt === "number" ? t.profileFetchedAt : 0
      const explicitRefresh = trigger === "update"
      const stale = !fetchedAt || Date.now() - fetchedAt > PROFILE_TTL_MS

      if (accessToken && (explicitRefresh || stale || t.isFirstTime !== false)) {
        const me = await fetchMe(accessToken)
        if (me) {
          t.tenantId = me.tenant_id || null
          t.profileType = me.profile_type || null
          t.isFirstTime = me.is_first_time
          t.role = me.role || null
          t.isOwner = me.is_owner
          t.profileFetchedAt = Date.now()
        }
      }
      return token
    },
    async session({ session, token }) {
      const t = token as Record<string, unknown>
      session.accessToken = typeof t.accessToken === "string" ? t.accessToken : undefined
      session.user = {
        ...session.user,
        id: typeof t.sub === "string" ? t.sub : "",
        tenantId: typeof t.tenantId === "string" ? t.tenantId : null,
        profileType: typeof t.profileType === "string" ? t.profileType : null,
        isFirstTime: typeof t.isFirstTime === "boolean" ? t.isFirstTime : true,
        role: typeof t.role === "string" ? t.role : null,
        isOwner: typeof t.isOwner === "boolean" ? t.isOwner : false,
      }
      return session
    },
  },
})
