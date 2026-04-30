"use client"

import { useEffect } from "react"

export interface GlobalShortcutOptions {
  /** Key to match (e.g. "k", "j"). Case-insensitive. */
  key: string
  /** Require Cmd (Mac) or Ctrl (Windows/Linux). Default true. */
  cmdOrCtrl?: boolean
  /** Callback fired when the shortcut triggers. */
  onTrigger: () => void
  /** Set to false to temporarily disable the shortcut. Default true. */
  enabled?: boolean
}

/**
 * useGlobalShortcut registers a global keydown handler.
 *
 * Cmd+K / Ctrl+K → open command palette
 * Cmd+J / Ctrl+J → open AI drawer
 *
 * Skips when the event target is an input/textarea/contenteditable to avoid
 * intercepting normal typing.
 */
export function useGlobalShortcut({
  key,
  cmdOrCtrl = true,
  onTrigger,
  enabled = true,
}: GlobalShortcutOptions): void {
  useEffect(() => {
    if (!enabled) return

    const handler = (e: KeyboardEvent) => {
      // Skip when user is typing in an input.
      const target = e.target as HTMLElement
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      ) {
        return
      }

      const mod = e.metaKey || e.ctrlKey
      if (cmdOrCtrl && !mod) return
      if (!cmdOrCtrl && mod) return

      if (e.key.toLowerCase() === key.toLowerCase()) {
        e.preventDefault()
        onTrigger()
      }
    }

    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [key, cmdOrCtrl, onTrigger, enabled])
}
