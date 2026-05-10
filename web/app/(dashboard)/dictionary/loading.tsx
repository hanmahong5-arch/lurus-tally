import { Skeleton } from "@/components/ui/skeleton"

// Dictionary entries — table skeleton.
export default function Loading() {
  return (
    <div className="flex flex-col gap-4 p-6">
      <span className="sr-only">加载中...</span>
      <Skeleton className="h-9 w-40" />
      <Skeleton className="h-10 w-full" />
      <div className="flex flex-col gap-2">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
      <Skeleton className="h-9 w-64 self-end" />
    </div>
  )
}
