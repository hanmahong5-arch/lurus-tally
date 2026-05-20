"use client"

import { useEffect, useState } from "react"

/**
 * OfflineBanner monitors the browser's online/offline events and shows a fixed
 * top banner when the network connection is lost. It disappears automatically
 * once connectivity is restored.
 *
 * Mount once in the dashboard layout; no props required.
 */
export function OfflineBanner() {
  const [offline, setOffline] = useState(false)

  useEffect(() => {
    // Initialise from current state in case component mounts while offline.
    setOffline(!navigator.onLine)

    const handleOffline = () => setOffline(true)
    const handleOnline = () => setOffline(false)

    window.addEventListener("offline", handleOffline)
    window.addEventListener("online", handleOnline)

    return () => {
      window.removeEventListener("offline", handleOffline)
      window.removeEventListener("online", handleOnline)
    }
  }, [])

  if (!offline) return null

  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed inset-x-0 top-0 z-[100] flex items-center justify-center bg-amber-400 px-4 py-2 text-sm font-medium text-amber-950 shadow-sm"
    >
      网络已断开，操作可能未保存
    </div>
  )
}
