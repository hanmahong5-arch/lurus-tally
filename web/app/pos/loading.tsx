import { Skeleton } from "@/components/ui/skeleton"

// POS main screen — product grid + cart panel skeleton.
export default function Loading() {
  return (
    <div className="grid h-screen grid-cols-1 gap-4 p-4 lg:grid-cols-[1fr_400px]">
      <span className="sr-only">加载中...</span>
      <div className="flex flex-col gap-4">
        <Skeleton className="h-12 w-full" />
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
          {Array.from({ length: 12 }).map((_, i) => (
            <Skeleton key={i} className="h-28 w-full" />
          ))}
        </div>
      </div>
      <div className="flex flex-col gap-3">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="flex-1 w-full" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-12 w-full" />
      </div>
    </div>
  )
}
