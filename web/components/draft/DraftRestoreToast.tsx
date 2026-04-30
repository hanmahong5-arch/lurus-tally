"use client"

interface DraftRestoreToastProps {
  /** Non-null when a draft was successfully loaded from IDB on mount. */
  restoredAt: Date | null
  /** Called when the user clicks "放弃" to discard the draft. */
  onDiscard: () => void
}

/**
 * DraftRestoreToast renders a banner at the top of a form when a draft was
 * automatically restored from IndexedDB.
 *
 * Displays how many minutes ago the draft was saved and provides a "放弃" action.
 * Renders nothing when restoredAt is null.
 */
export function DraftRestoreToast({ restoredAt, onDiscard }: DraftRestoreToastProps) {
  if (!restoredAt) return null

  const minutesAgo = Math.max(
    1,
    Math.round((Date.now() - restoredAt.getTime()) / 60_000)
  )

  return (
    <div className="flex items-center justify-between rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-2.5 text-sm text-amber-700 dark:text-amber-400">
      <span>已恢复 {minutesAgo} 分钟前的草稿</span>
      <button
        type="button"
        onClick={onDiscard}
        className="ml-4 text-xs underline underline-offset-2 hover:opacity-70 transition-opacity"
      >
        放弃
      </button>
    </div>
  )
}
