import { Skeleton } from "@/components/ui/skeleton"

// Overview page: KPI card grid skeleton.
export default function Loading() {
  return (
    <div className="flex flex-col gap-6 p-6">
      <span className="sr-only">加载中...</span>
      <Skeleton className="h-9 w-56" />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-28 w-full" />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Skeleton className="h-72 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    </div>
  )
}
