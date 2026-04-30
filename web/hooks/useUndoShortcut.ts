"use client"

import { useEffect } from "react"
import { globalUndoStack, type UndoEntry } from "@/lib/undo/undo-stack"
import { trackEvent } from "@/lib/telemetry"

export interface UseUndoShortcutOptions {
  /** Called with the popped entry when an undo succeeds. */
  onUndo: (entry: UndoEntry) => void
  /** Called when Cmd+Z fires but the stack is empty or all entries expired. */
  onEmptyStack: () => void
}

/**
 * useUndoShortcut registers a global Cmd+Z / Ctrl+Z handler.
 *
 * The shortcut is skipped when the event target is an INPUT, TEXTAREA, or
 * contentEditable element — mirroring the guard in useGlobalShortcut.ts.
 */
export function useUndoShortcut({ onUndo, onEmptyStack }: UseUndoShortcutOptions): void {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Skip when user is typing in a form field.
      const target = e.target as HTMLElement
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      ) {
        return
      }

      if ((e.metaKey || e.ctrlKey) && e.key === "z") {
        e.preventDefault()

        const entry = globalUndoStack.pop()
        if (entry) {
          // Fire-and-forget: revert action is async; errors are swallowed per spec.
          entry.action.revert().catch(() => undefined)
          trackEvent("undo_used")
          onUndo(entry)
        } else {
          onEmptyStack()
        }
      }
    }

    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [onUndo, onEmptyStack])
}
