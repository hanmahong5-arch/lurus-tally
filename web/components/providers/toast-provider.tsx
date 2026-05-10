'use client'

import { Toaster, toast } from 'sonner'
import { useEffect, useRef, useState } from 'react'

/**
 * ToastProvider mounts the global sonner Toaster and a top-of-page banner
 * that surfaces network connectivity loss. Place inside ThemeProvider so the
 * toast stack inherits the theme class.
 */
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [online, setOnline] = useState(true)
  const wasOffline = useRef(false)

  useEffect(() => {
    const update = () => {
      const isOnline = navigator.onLine
      setOnline(isOnline)
      if (!isOnline) {
        wasOffline.current = true
      } else if (wasOffline.current) {
        wasOffline.current = false
        toast.success('网络已恢复')
      }
    }
    update()
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    return () => {
      window.removeEventListener('online', update)
      window.removeEventListener('offline', update)
    }
  }, [])

  return (
    <>
      {!online && (
        <div
          role="status"
          aria-live="polite"
          className="fixed top-0 inset-x-0 bg-amber-600 text-white text-sm py-1 text-center z-50"
        >
          网络已断开 — 您的修改将无法保存
        </div>
      )}
      {children}
      <Toaster position="top-right" theme="dark" richColors closeButton />
    </>
  )
}
