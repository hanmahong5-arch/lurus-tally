'use client'

import { Toaster, toast } from 'sonner'
import { useEffect, useRef } from 'react'

const FLASH_COOKIE = 'tally-flash'

type FlashLevel = 'success' | 'info' | 'warning' | 'error'

function consumeFlashCookie(): { level: FlashLevel; text: string } | null {
  if (typeof document === 'undefined') return null
  const match = document.cookie.split('; ').find(c => c.startsWith(FLASH_COOKIE + '='))
  if (!match) return null
  // Clear immediately so a refresh does not retrigger the toast.
  document.cookie = `${FLASH_COOKIE}=; path=/; max-age=0; sameSite=lax`
  try {
    const payload = JSON.parse(decodeURIComponent(match.slice(FLASH_COOKIE.length + 1)))
    if (typeof payload?.text === 'string') {
      const level: FlashLevel = ['success', 'info', 'warning', 'error'].includes(payload.level)
        ? payload.level
        : 'info'
      return { level, text: payload.text }
    }
  } catch {
    // Malformed cookie — drop silently
  }
  return null
}

/**
 * ToastProvider mounts the global sonner Toaster and consumes one-shot
 * `tally-flash` cookies set by middleware redirects so the user sees a reason
 * for the bounce. The persistent "you are offline" banner lives solely in
 * components/ui/offline-banner.tsx (mounted once, app-wide) — this provider
 * only fires a one-shot toast when connectivity is restored, so the two
 * implementations never race or disagree on copy/colour.
 */
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const wasOffline = useRef(false)

  useEffect(() => {
    const flash = consumeFlashCookie()
    if (flash) {
      const fn = flash.level === 'warning' ? toast.warning : toast[flash.level]
      fn(flash.text)
    }
    const update = () => {
      const isOnline = navigator.onLine
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
      {children}
      <Toaster position="top-right" theme="dark" richColors closeButton />
    </>
  )
}
