"use client"

import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

interface ErrorBannerProps {
  /** Primary message — the "what went wrong" sentence. */
  children: ReactNode
  /** Secondary hint (e.g. "请稍后再试"). Optional. */
  hint?: ReactNode
  /** When provided, a small ✕ button renders that calls this on click. */
  onDismiss?: () => void
  className?: string
}

/**
 * ErrorBanner is the project's single error-callout. Replaces every ad-hoc
 * `<div className="rounded-{md|lg} bg-destructive/10 ...">` block — one radius,
 * one border weight, one opacity. Keep it for inline errors; route global
 * failures through sonner toast instead.
 */
export function ErrorBanner({ children, hint, onDismiss, className }: ErrorBannerProps) {
  return (
    <div
      role="alert"
      className={cn(
        "flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive",
        className,
      )}
    >
      <div className="flex-1">
        <p className="font-medium">{children}</p>
        {hint && <p className="mt-0.5 text-xs text-destructive/80">{hint}</p>}
      </div>
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          aria-label="关闭"
          className="text-destructive/70 hover:text-destructive transition-colors disabled:opacity-50"
        >
          ✕
        </button>
      )}
    </div>
  )
}
