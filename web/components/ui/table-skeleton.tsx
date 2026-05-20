"use client"

import { Skeleton } from "@/components/ui/skeleton"

interface TableSkeletonProps {
  /** Number of placeholder rows to render (default 5). */
  rows?: number
  /** Number of columns per row (default 4). */
  cols?: number
}

/**
 * TableSkeleton renders a shimmer placeholder for table-based list pages.
 * Used to replace "加载中..." text during data fetch, keeping layout stable.
 */
export function TableSkeleton({ rows = 5, cols = 4 }: TableSkeletonProps) {
  const colWidths = ["w-20", "w-32", "w-24", "w-16", "w-20", "w-12"]

  return (
    <div className="overflow-x-auto rounded-xl border border-border">
      {/* Header row */}
      <div className="flex gap-4 border-b border-border bg-muted/50 px-4 py-2.5">
        {Array.from({ length: cols }).map((_, ci) => (
          <Skeleton
            key={ci}
            className={`h-4 bg-muted-foreground/15 ${colWidths[ci % colWidths.length]}`}
          />
        ))}
      </div>

      {/* Data rows */}
      {Array.from({ length: rows }).map((_, ri) => (
        <div
          key={ri}
          className="flex gap-4 border-b border-border px-4 py-3 last:border-b-0"
        >
          {Array.from({ length: cols }).map((_, ci) => (
            <Skeleton
              key={ci}
              className={`h-4 bg-muted-foreground/10 ${colWidths[(ci + ri) % colWidths.length]}`}
            />
          ))}
        </div>
      ))}
    </div>
  )
}
