"use client"

import { useEffect, useRef, type RefObject } from "react"

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

/**
 * useFocusTrap keeps keyboard focus (Tab / Shift+Tab) cycling within the given
 * container while `active` is true, and returns focus to whatever had it
 * beforehand once deactivated.
 *
 * Base UI's Dialog gives this for free to anything built on Dialog.Root/Popup
 * (see components/ui/sheet.tsx, modal.tsx). This hook is the lightweight
 * fallback for the two hand-rolled dialog-like panels that predate that
 * pattern (AI Drawer, Command Palette) — without it, Tab leaks focus onto
 * the page behind an open panel, which breaks the aria-modal contract.
 */
export function useFocusTrap(containerRef: RefObject<HTMLElement | null>, active: boolean): void {
  const previouslyFocused = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!active) return
    const container = containerRef.current
    if (!container) return

    previouslyFocused.current = document.activeElement as HTMLElement | null

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return
      const focusable = Array.from(
        container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)
      ).filter((el) => el.offsetParent !== null)
      if (focusable.length === 0) return

      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const activeEl = document.activeElement as HTMLElement | null

      if (e.shiftKey) {
        if (activeEl === first || !container.contains(activeEl)) {
          e.preventDefault()
          last.focus()
        }
      } else if (activeEl === last || !container.contains(activeEl)) {
        e.preventDefault()
        first.focus()
      }
    }

    document.addEventListener("keydown", handleKeyDown)
    return () => {
      document.removeEventListener("keydown", handleKeyDown)
      // Return focus to whatever opened the panel (e.g. the FAB / ⌘K trigger).
      previouslyFocused.current?.focus?.()
    }
  }, [active, containerRef])
}
