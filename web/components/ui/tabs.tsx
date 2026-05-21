"use client"

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export interface TabItem<V> {
  label: ReactNode
  value: V
  /** Optional trailing count / badge. */
  badge?: ReactNode
}

interface TabsProps<V> {
  items: TabItem<V>[]
  value: V
  onValueChange: (value: V) => void
  /** `underline` for page-level filters, `segment` for compact toggles. */
  variant?: "underline" | "segment"
  className?: string
}

/**
 * Tabs is the single controlled tab strip — one active-state style across the
 * app (`border-primary text-primary`). It renders only the strip; the consumer
 * decides what to show for the active value, so it works for filter tabs (no
 * panels, reload on change) and content tabs alike.
 */
export function Tabs<V extends string | number | undefined>({
  items,
  value,
  onValueChange,
  variant = "underline",
  className,
}: TabsProps<V>) {
  if (variant === "segment") {
    return (
      <div
        role="tablist"
        className={cn(
          "inline-flex items-center gap-1 rounded-lg border border-border bg-muted/40 p-1",
          className
        )}
      >
        {items.map((tab, i) => {
          const active = tab.value === value
          return (
            <button
              key={i}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => onValueChange(tab.value)}
              className={cn(
                "rounded-md px-3 py-1 text-sm transition-colors",
                active
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {tab.label}
              {tab.badge != null && (
                <span className="ml-1.5 text-xs text-muted-foreground">{tab.badge}</span>
              )}
            </button>
          )
        })}
      </div>
    )
  }

  return (
    <div role="tablist" className={cn("flex gap-1 border-b border-border", className)}>
      {items.map((tab, i) => {
        const active = tab.value === value
        return (
          <button
            key={i}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onValueChange(tab.value)}
            className={cn(
              "-mb-px border-b-2 px-4 py-2 text-sm transition-colors",
              active
                ? "border-primary font-medium text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            {tab.label}
            {tab.badge != null && <span className="ml-1.5">{tab.badge}</span>}
          </button>
        )
      })}
    </div>
  )
}
