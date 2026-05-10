import { Skeleton } from "@/components/ui/skeleton"

// Generic dashboard content fallback (sidebar already rendered by layout).
export default function Loading() {
  return (
    <div className="flex flex-col gap-4 p-6">
      <span className="sr-only">加载中...</span>
      <Skeleton className="h-9 w-48" />
      <Skeleton className="h-10 w-full max-w-md" />
      <div className="flex flex-col gap-2">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
    </div>
  )
}
