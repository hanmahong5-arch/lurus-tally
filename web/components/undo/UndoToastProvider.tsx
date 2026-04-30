"use client"

import { useState, useCallback, useRef } from "react"
import { useUndoShortcut } from "@/hooks/useUndoShortcut"
import { type UndoEntry } from "@/lib/undo/undo-stack"

/** Auto-dismiss duration for a successful undo toast (ms). */
const UNDO_DISMISS_MS = 4_000
/** Auto-dismiss duration for the empty-stack toast (ms). */
const EMPTY_DISMISS_MS = 2_000

interface ToastState {
  message: string
  visible: boolean
}

/**
 * UndoToastProvider wraps its children and renders a global undo toast overlay.
 *
 * Mount once at the dashboard layout level. It listens for Cmd+Z / Ctrl+Z globally
 * and shows feedback toasts for both successful undos and empty-stack attempts.
 */
export function UndoToastProvider({ children }: { children: React.ReactNode }) {
  const [toast, setToast] = useState<ToastState>({ message: "", visible: false })
  const dismissTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  function showToast(message: string, durationMs: number) {
    if (dismissTimerRef.current !== null) {
      clearTimeout(dismissTimerRef.current)
    }
    setToast({ message, visible: true })
    dismissTimerRef.current = setTimeout(() => {
      setToast((prev) => ({ ...prev, visible: false }))
      dismissTimerRef.current = null
    }, durationMs)
  }

  const handleUndo = useCallback((entry: UndoEntry) => {
    const label =
      entry.action.type === "delete_product"
        ? `「${entry.action.name}」已撤销`
        : `「${entry.action.billNo}」已撤销`
    showToast(label, UNDO_DISMISS_MS)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleEmptyStack = useCallback(() => {
    showToast("没有可撤销的操作", EMPTY_DISMISS_MS)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useUndoShortcut({ onUndo: handleUndo, onEmptyStack: handleEmptyStack })

  return (
    <>
      {children}
      {toast.visible && (
        <div
          role="status"
          aria-live="polite"
          className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 rounded-lg bg-zinc-800 text-white px-4 py-2.5 text-sm shadow-lg"
        >
          {toast.message}
        </div>
      )}
    </>
  )
}
