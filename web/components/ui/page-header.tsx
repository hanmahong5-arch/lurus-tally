import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

interface PageHeaderProps {
  title: ReactNode
  subtitle?: ReactNode
  /** Right-aligned action slot (buttons, links). */
  actions?: ReactNode
  /** Optional breadcrumb row rendered above the title. */
  breadcrumb?: ReactNode
  className?: string
}

/**
 * PageHeader is the single page-title block — replaces the 3+ hand-rolled header
 * layouts across dashboard pages. RSC-safe (no hooks).
 */
export function PageHeader({ title, subtitle, actions, breadcrumb, className }: PageHeaderProps) {
  return (
    <div className={cn("mb-6", className)}>
      {breadcrumb && <div className="mb-2 text-xs text-muted-foreground">{breadcrumb}</div>}
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold tracking-tight text-foreground">
            {title}
          </h1>
          {subtitle && <p className="mt-0.5 text-sm text-muted-foreground">{subtitle}</p>}
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}
