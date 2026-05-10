import { cn } from "@/lib/utils"

// Generic skeleton primitive — pulse box for placeholder content.
export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("animate-pulse rounded-md bg-white/10", className)} {...props} />
}
