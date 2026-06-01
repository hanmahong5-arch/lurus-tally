"use client"

import { useSession } from "next-auth/react"

/**
 * useTenantId returns the current tenant id from the NextAuth session, or
 * `undefined` while the session is loading or when there is no tenant.
 *
 * It NEVER falls back to a hard-coded dev tenant id: the dev tenant only ever
 * reaches a client via the session (the offline Credentials provider injects
 * it server-side). Leaking a literal tenant id into the bundle would let every
 * visitor inherit that tenant context — the bug `next.config.mjs` guards
 * against. Callers pass the result as `tenantId`; in production the value is an
 * X-Tenant-ID hint that the API proxy strips, with the backend deriving the
 * real tenant from the JWT.
 */
export function useTenantId(): string | undefined {
  // Optional-chain the whole result: with the <SessionProvider> mounted in the
  // root layout useSession() always returns an object, but a page rendered
  // outside the provider (or a static prerender) would otherwise get undefined
  // and throw on destructure. Degrading to undefined is safe — tenantId is only
  // an X-Tenant-ID hint that the proxy strips.
  const session = useSession()
  return session?.data?.user?.tenantId ?? undefined
}
