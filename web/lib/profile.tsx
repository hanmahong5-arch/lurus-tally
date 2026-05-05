"use client"

import { createContext, useContext } from "react"

export type ProfileType = "cross_border" | "retail" | "hybrid" | "horticulture" | null

export interface ProfileContextValue {
  profileType: ProfileType
}

const ProfileContext = createContext<ProfileContextValue>({ profileType: null })

/**
 * ProfileProvider wraps a subtree with a known profileType value.
 * Wired in app/(dashboard)/layout.tsx via NextAuth `auth()` →
 * `session.user.profileType` (populated by jwt callback fetching /api/v1/me).
 */
export function ProfileProvider({
  value,
  children,
}: {
  value: ProfileContextValue
  children: React.ReactNode
}) {
  return (
    <ProfileContext.Provider value={value}>{children}</ProfileContext.Provider>
  )
}

/**
 * useProfile returns the current profile type from context.
 *
 * Falls back to `cross_border` only when the context value is null — this
 * happens in dev mode without an authenticated session, or in unit tests
 * that don't wrap a ProfileProvider. Production-authenticated users always
 * have a non-null profileType (middleware redirects first-time users to /setup
 * before any dashboard route renders).
 */
export function useProfile(): ProfileContextValue {
  const ctx = useContext(ProfileContext)
  if (ctx.profileType === null) {
    return { profileType: "cross_border" }
  }
  return ctx
}

/**
 * ProfileGate renders children only when the current profileType is in the
 * allowed profiles list. Use this for profile-specific UI sections.
 */
export function ProfileGate({
  profiles,
  children,
}: {
  profiles: ProfileType[]
  children: React.ReactNode
}) {
  const { profileType } = useProfile()
  if (!profiles.includes(profileType)) {
    return null
  }
  return <>{children}</>
}
