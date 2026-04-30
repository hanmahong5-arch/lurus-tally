"use client"

import { isDraftStorageAvailable } from "@/lib/draft/idb-storage"
import type { DraftStatus } from "@/hooks/useDraft"

interface DraftBadgeProps {
  status: DraftStatus
}

/**
 * DraftBadge displays the current draft persistence state inline in a form header.
 *
 * Shows nothing when IndexedDB storage is unavailable (private-browsing mode).
 */
export function DraftBadge({ status }: DraftBadgeProps) {
  if (!isDraftStorageAvailable()) return null
  if (status === "none") return null

  const label = status === "local" ? "未保存" : "已同步"
  const colorCls =
    status === "local"
      ? "bg-amber-500/15 text-amber-600 dark:text-amber-400"
      : "bg-green-500/15 text-green-700 dark:text-green-400"

  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${colorCls}`}
    >
      {label}
    </span>
  )
}
