"use client"

import { useEffect } from "react"

/** Registers the minimal service worker in production builds only. */
export function PwaRegister() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production") return
    if (!("serviceWorker" in navigator)) return
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // Installability is a nicety; a failed registration must not bother the user.
    })
  }, [])
  return null
}
