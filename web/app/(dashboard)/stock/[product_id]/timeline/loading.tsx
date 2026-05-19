import { Skeleton } from "@/components/ui/skeleton"

export default function Loading() {
  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <span className="sr-only">加载中...</span>
      {/* Breadcrumb */}
      <Skeleton className="h-4 w-48" />
      {/* Header */}
      <div className="space-y-1.5">
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-4 w-64" />
      </div>
      {/* Timeline rows */}
      {Array.from({ length: 8 }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-4 rounded-lg border border-border bg-card px-4 py-3"
        >
          <Skeleton className="h-5 w-12 rounded-full flex-shrink-0" />
          <Skeleton className="h-4 w-20 flex-shrink-0" />
          <Skeleton className="h-4 flex-1" />
          <Skeleton className="h-4 w-28 flex-shrink-0" />
        </div>
      ))}
    </div>
  )
}
