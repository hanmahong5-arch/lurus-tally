import { Skeleton } from "@/components/ui/skeleton"

// Setup wizard — three persona/profile cards skeleton.
export default function Loading() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-8 p-6">
      <span className="sr-only">加载中...</span>
      <Skeleton className="h-10 w-72" />
      <Skeleton className="h-5 w-96" />
      <div className="grid w-full max-w-5xl grid-cols-1 gap-4 md:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-72 w-full" />
        ))}
      </div>
    </div>
  )
}
