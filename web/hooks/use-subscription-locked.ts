"use client"

import { useEffect, useState } from "react"

import { getSubscriptionLockState } from "@/lib/api/billing"

const REFRESH_MS = 5 * 60 * 1000

/**
 * useSubscriptionLocked reads the same status source as the back-office banner.
 * Any failure reads as "not locked": a platform hiccup must never stop a sale.
 */
export function useSubscriptionLocked(): boolean {
  const [locked, setLocked] = useState(false)
  useEffect(() => {
    let alive = true
    const load = () => {
      getSubscriptionLockState()
        .then((st) => {
          if (alive) setLocked(st.locked === true)
        })
        .catch(() => undefined)
    }
    load()
    const timer = setInterval(load, REFRESH_MS)
    window.addEventListener("tally:subscription-refreshed", load)
    return () => {
      alive = false
      clearInterval(timer)
      window.removeEventListener("tally:subscription-refreshed", load)
    }
  }, [])
  return locked
}

export const LOCKED_HINT = "套餐已到期，现在只能查看，请联系老板续费"
