'use client'

import { Toaster, toast } from 'sonner'
import { useEffect, useRef, useState } from 'react'

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
 * ToastProvider mounts the global sonner Toaster and a top-of-page banner
 * that surfaces network connectivity loss. Also consumes one-shot `tally-flash`
 * cookies set by middleware redirects so the user sees a reason for the bounce.
 */
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [online, setOnline] = useState(true)
  const wasOffline = useRef(false)

  useEffect(() => {
    const flash = consumeFlashCookie()
    if (flash) {
      const fn = flash.level === 'warning' ? toast.warning : toast[flash.level]
      fn(flash.text)
    }
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
