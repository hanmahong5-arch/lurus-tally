import { useEffect } from "react"

interface PosHotkeysOptions {
  searchRef: React.RefObject<HTMLInputElement | null>
  lastQtyRef: React.RefObject<HTMLInputElement | null>
  onPayRequested: () => void
  onCancelRequested: () => void
}

/**
 * usePosHotkeys binds F1-F4 keyboard shortcuts for the POS interface.
 *
 * F1 — focus the product search input
 * F2 — focus the last-added item quantity input
 * F3 — open the payment modal (calls onPayRequested)
 * F4 — cancel / clear cart (calls onCancelRequested)
 *
 * ESC is intentionally excluded — modal components handle it via their own onOpenChange.
 */
export function usePosHotkeys({
  searchRef,
  lastQtyRef,
  onPayRequested,
  onCancelRequested,
}: PosHotkeysOptions): void {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      switch (e.key) {
        case "F1":
          e.preventDefault()
          searchRef.current?.focus()
          break
        case "F2":
          e.preventDefault()
          lastQtyRef.current?.focus()
          break
        case "F3":
          e.preventDefault()
          onPayRequested()
          break
        case "F4":
          e.preventDefault()
          onCancelRequested()
          break
        default:
          break
      }
    }

    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [searchRef, lastQtyRef, onPayRequested, onCancelRequested])
}
