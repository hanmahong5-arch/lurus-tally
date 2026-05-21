import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

const WIDTHS = {
  narrow: "max-w-3xl",
  default: "max-w-5xl",
  wide: "max-w-7xl",
  full: "max-w-none",
} as const

interface PageContainerProps {
  /** Content max width preset. Ends the p-6 / px-6 py-8 / max-w-* drift. */
  width?: keyof typeof WIDTHS
  className?: string
  children: ReactNode
}

/**
 * PageContainer is the single page-level wrapper: one padding rhythm, one set of
 * width presets. RSC-safe (no hooks) so server and client pages share it.
 */
export function PageContainer({ width = "default", className, children }: PageContainerProps) {
  return (
    <div className={cn("mx-auto w-full px-6 py-6", WIDTHS[width], className)}>
      {children}
    </div>
  )
}
