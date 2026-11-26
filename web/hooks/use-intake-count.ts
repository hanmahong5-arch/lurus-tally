"use client"

import { useEffect, useState } from "react"
import { intakeCount } from "@/lib/api/intake"

/** Sidebar badge. Stays quiet when this person cannot capture, so cashiers never see a 403. */
export function useIntakeCount(enabled: boolean, pathname: string | null): number {
  const [n, setN] = useState(0)
  useEffect(() => {
    if (!enabled) return
    const ctrl = new AbortController()
    intakeCount(ctrl.signal).then((c) => { if (!ctrl.signal.aborted) setN(c) }).catch(() => {})
    return () => ctrl.abort()
  }, [enabled, pathname])
  return n
}
