import type { ComponentProps } from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap",
  {
    variants: {
      tone: {
        neutral: "bg-muted text-muted-foreground",
        ok: "bg-success/10 text-success",
        warn: "bg-warning/10 text-warning",
        err: "bg-destructive/10 text-destructive",
        info: "bg-info/10 text-info",
        accent: "bg-primary/10 text-primary",
      },
    },
    defaultVariants: {
      tone: "neutral",
    },
  }
)

export type BadgeTone = NonNullable<VariantProps<typeof badgeVariants>["tone"]>

function Badge({
  className,
  tone,
  ...props
}: ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return (
    <span data-slot="badge" className={cn(badgeVariants({ tone }), className)} {...props} />
  )
}

export { Badge, badgeVariants }
