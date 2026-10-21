"use client"

import { useSession } from "next-auth/react"

import { normalizeRole, roleCan, type Perm, type ShopRole } from "@/lib/roles"

export { normalizeRole, roleCan, roleLabel } from "@/lib/roles"
export type { Perm, ShopRole } from "@/lib/roles"

export interface RoleState {
  role: ShopRole | null
  /** False until the session has told us the role. */
  known: boolean
  can: (perm: Perm) => boolean
}

/**
 * useRole returns the signed-in person's shop role from the session. The
 * backend still enforces every permission; this only decides what to show.
 */
export function useRole(): RoleState {
  const session = useSession()
  const user = session?.data?.user
  const role = normalizeRole(user?.role, user?.isOwner === true)
  return { role, known: role !== null, can: (perm: Perm) => roleCan(role, perm) }
}
