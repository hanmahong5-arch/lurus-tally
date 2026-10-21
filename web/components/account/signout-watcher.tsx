"use client"

import { useSignOutBroadcast } from "@/hooks/use-signout-broadcast"

/** SignOutWatcher lets a server layout listen for the shared device's sign-out; it renders nothing. */
export function SignOutWatcher() {
  useSignOutBroadcast()
  return null
}
