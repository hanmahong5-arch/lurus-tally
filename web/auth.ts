import NextAuth from "next-auth"
import Credentials from "next-auth/providers/credentials"
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

// DEV_TENANT_ID is the fixed tenant used by the offline dev/E2E Credentials
// provider. Choosing a predictable value lets E2E specs assert tenantId without
// dynamic lookup.
const DEV_TENANT_ID = "00000000-0000-0000-0000-000000000001"
// DEV_USER_ID is the fixed user id returned by the dev provider. Predictable
// value allows E2E specs to assert session.user.id without dynamic lookup.
const DEV_USER_ID = "00000000-0000-0000-0000-000000000002"

// devProviderEnabled checks the two required conditions before activating the
// offline Credentials provider. Both must be true — neither alone is sufficient.
// Production environments MUST never have this provider active regardless of
// what env vars are present.
//
// Gate 1: AUTH_DEV_PROVIDER === "true"   — explicit opt-in
// Gate 2: NODE_ENV !== "production"      — production hard-block
//
// If someone accidentally sets AUTH_DEV_PROVIDER=true in production, we emit a
// console.error and refuse to activate the provider. This is a production-safety
// red line — dev sessions must never work in production.
function devProviderEnabled(): boolean {
  const opted = process.env.AUTH_DEV_PROVIDER === "true"
  if (!opted) return false

  const isProd = process.env.NODE_ENV === "production"
  if (isProd) {
    // Production safety gate — refuse unconditionally and alert operators.
    console.error(
      "[auth] DANGER: AUTH_DEV_PROVIDER=true detected in production. " +
        "The offline dev Credentials provider will NOT be activated. " +
        "Remove AUTH_DEV_PROVIDER from your production environment immediately.",
    )
    return false
  }

  return true
}

// demoProviderEnabled gates the public no-OIDC sandbox credentials provider.
// Unlike the dev provider it is DELIBERATELY allowed in production (NODE_ENV is
// not checked): the sandbox is a public feature switched on per-deployment by the
// same operator flag the backend reads (TALLY_DEMO_MODE). Safe in production
// because the session it mints is useless on its own — every backend call is
// gated by the demo PAT it carries, which the backend validates (real, active,
// tenant-scoped) on each request; a forged session with a bogus token just 401s.
// Off by default; an operator must opt in.
function demoProviderEnabled(): boolean {
  return process.env.TALLY_DEMO_MODE === "true"
}

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

// devCredentialsProvider returns an offline Credentials provider that accepts
// any email + optional tenantId and issues a fixed dev session. This provider
// is only included in the providers list when devProviderEnabled() returns true.
// It does NOT call the backend — it is an entirely offline stub for UAT/E2E use.
function devCredentialsProvider() {
  return Credentials({
    id: "credentials",
    name: "Dev / E2E (offline)",
    credentials: {
      email: { label: "Email", type: "email" },
      tenantId: { label: "Tenant ID", type: "text" },
      // Optional backend bearer (e.g. a PAT) for STAGE UAT: when present the
      // session carries it as accessToken so the /api/proxy/* route forwards
      // real authenticated requests instead of having no token at all.
      accessToken: { label: "Backend access token", type: "password" },
    },
    async authorize(raw) {
      const email = typeof raw?.email === "string" && raw.email ? raw.email : "dev@tally.test"
      const tenantId =
        typeof raw?.tenantId === "string" && raw.tenantId ? raw.tenantId : DEV_TENANT_ID
      const accessToken =
        typeof raw?.accessToken === "string" && raw.accessToken ? raw.accessToken : undefined

      return {
        // Fixed, predictable values so E2E assertions are deterministic.
        id: DEV_USER_ID,
        email,
        name: "Dev User",
        // Extra fields stored in the JWT via the jwt() callback below.
        devTenantId: tenantId,
        devAccessToken: accessToken,
      }
    },
  })
}

// demoCredentialsProvider is the public sandbox provider. It is handed the
// {tenantId, accessToken} that the backend's POST /api/v1/demo/start just minted
// (an ephemeral tenant + a short-lived PAT) and turns them into a session whose
// accessToken is that PAT — so /api/proxy forwards it as the bearer and the
// dashboard works without OIDC. profileType is forced to "horticulture" so the
// nursery vertical presents and middleware does not bounce the visitor to /setup.
// Returns null on missing inputs so no blank session is ever created.
function demoCredentialsProvider() {
  return Credentials({
    id: "demo",
    name: "Demo Sandbox (no-OIDC)",
    credentials: {
      tenantId: { label: "Tenant ID", type: "text" },
      accessToken: { label: "Demo PAT", type: "password" },
    },
    async authorize(raw) {
      const tenantId = typeof raw?.tenantId === "string" && raw.tenantId ? raw.tenantId : ""
      const accessToken =
        typeof raw?.accessToken === "string" && raw.accessToken ? raw.accessToken : ""
      if (!tenantId || !accessToken) return null
      return {
        id: "demo-" + tenantId,
        email: "demo@tally.sandbox",
        name: "Demo",
        // Reuse the dev/credentials jwt branch (keyed on devTenantId); demoProfile
        // distinguishes the nursery sandbox from the dev provider's cross_border.
        devTenantId: tenantId,
        devAccessToken: accessToken,
        demoProfile: "horticulture",
      }
    },
  })
}

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Zitadel({
      clientId: process.env.ZITADEL_CLIENT_ID!,
      issuer: process.env.ZITADEL_ISSUER ?? "https://identity.lurus.cn",
      // PKCE is enabled by default for Zitadel provider.
    }),
    // Dev/E2E Credentials provider — conditionally appended. Production gate
    // is enforced inside devProviderEnabled(); the provider is never present in
    // the array unless both NODE_ENV !== "production" AND AUTH_DEV_PROVIDER=true.
    ...(devProviderEnabled() ? [devCredentialsProvider()] : []),
    // Public no-OIDC sandbox provider — gated by TALLY_DEMO_MODE (prod-allowed).
    ...(demoProviderEnabled() ? [demoCredentialsProvider()] : []),
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
    async jwt({ token, account, profile, user, trigger }) {
      const t = token as Record<string, unknown>

      // Dev/E2E offline path: Credentials provider sign-in.
      // The `user` object from authorize() carries devTenantId; we detect this
      // path by the absence of an OIDC account object.
      const devUser = user as
        | (typeof user & { devTenantId?: string; devAccessToken?: string; demoProfile?: string })
        | undefined
      if (devUser?.devTenantId !== undefined) {
        t.sub = typeof user?.id === "string" ? user.id : DEV_USER_ID
        t.tenantId = devUser.devTenantId
        // demo sandbox → horticulture; dev/E2E provider → cross_border (default).
        t.profileType = devUser.demoProfile ?? "cross_border"
        t.isFirstTime = false
        t.role = "owner"
        t.isOwner = true
        t.profileFetchedAt = Date.now()
        // Offline dev sessions normally carry no accessToken. STAGE UAT may
        // hand the dev provider a backend bearer (PAT) — pass it through so
        // /api/proxy/* forwards authenticated requests. This branch only runs
        // when devTenantId is set, i.e. never on the Zitadel sign-in path,
        // and the provider itself is absent in production (double gate above).
        if (typeof devUser.devAccessToken === "string") {
          t.accessToken = devUser.devAccessToken
        }
        return token
      }

      // Production OIDC path (Zitadel) — unchanged below this line.

      // First sign-in: capture sub + id_token from the OIDC account.
      // We use id_token (always JWT, OIDC standard) rather than access_token
      // because Zitadel issues opaque access_tokens by default — backend would
      // be unable to validate them as JWT without enabling app-level "JWT
      // access tokens" in Zitadel app config.
      if (account && profile) {
        if (typeof profile.sub === "string") {
          t.sub = profile.sub
        }
        if (typeof account.id_token === "string") {
          t.accessToken = account.id_token
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
