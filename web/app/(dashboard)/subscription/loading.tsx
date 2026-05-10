import { Skeleton } from "@/components/ui/skeleton"

// Subscription/billing — card skeleton.
export default function Loading() {
  return (
    <div className="flex flex-col gap-6 p-6">
      <span className="sr-only">加载中...</span>
      <Skeleton className="h-9 w-48" />
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-64 w-full" />
        ))}
      </div>
      <Skeleton className="h-40 w-full" />
    </div>
  )
}
