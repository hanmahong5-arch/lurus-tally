"use client"

import { createContext, useContext } from "react"

export type ProfileType = "cross_border" | "retail" | "hybrid" | "horticulture" | null

export interface ProfileContextValue {
  profileType: ProfileType
}

const ProfileContext = createContext<ProfileContextValue>({ profileType: null })

/**
 * ProfileProvider wraps a subtree with a known profileType value.
 * Story 2.1 TODO: replace stub with real NextAuth session value.
 * When Story 2.1 is done, update the dashboard layout to call
 * `auth()` (server-side), extract `session.user.profileType`,
 * and pass it as `value` to ProfileProvider.
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
 * useProfile returns the current profile type.
 *
 * STUB: always returns { profileType: 'cross_border' } until Story 2.1
 * implements real NextAuth session integration.
 *
 * Story 2.1 TODO: remove the hardcoded stub below. The ProfileProvider
 * in the dashboard layout will supply the real value from the session.
 */
export function useProfile(): ProfileContextValue {
  const ctx = useContext(ProfileContext)
  // Stub: fall back to cross_border for development.
  // Story 2.1 TODO: remove this override — trust the context value directly.
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
