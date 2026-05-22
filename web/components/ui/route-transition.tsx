"use client"

import type { ReactNode } from "react"
import { usePathname } from "next/navigation"

/**
 * RouteTransition fades content in on navigation. Keying the wrapper by pathname
 * remounts it each route change so the `fade-in` keyframe replays (enter-only,
 * no exit — App Router unmounts the old tree immediately). Must live as a client
 * child of the Server-Component dashboard layout. The fade is disabled under
 * `prefers-reduced-motion` by the global gate in globals.css.
 */
export function RouteTransition({ children }: { children: ReactNode }) {
  const pathname = usePathname()
  return (
    <div key={pathname} className="animate-fade-in">
      {children}
    </div>
  )
}
