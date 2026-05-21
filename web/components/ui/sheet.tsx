"use client"

import type { ReactNode } from "react"
import { Dialog } from "@base-ui/react/dialog"

import { cn } from "@/lib/utils"

export interface SheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title?: ReactNode
  description?: ReactNode
  children?: ReactNode
  footer?: ReactNode
  side?: "right" | "left"
  className?: string
}

/**
 * Sheet is the single side-drawer — one width (`w-full sm:max-w-md`), one
 * slide transition driven by Base UI's `data-starting/ending-style`. Use it for
 * detail panels and inline editors; it reconciles the 420/480/full-width
 * drawers the audit found.
 */
export function Sheet({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  side = "right",
  className,
}: SheetProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/50 transition-opacity duration-200 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup
          className={cn(
            "fixed inset-y-0 z-50 flex w-full flex-col bg-background shadow-xl outline-none sm:max-w-md",
            "transition-transform duration-200 ease-out",
            side === "right"
              ? "right-0 border-l border-border data-[ending-style]:translate-x-full data-[starting-style]:translate-x-full"
              : "left-0 border-r border-border data-[ending-style]:-translate-x-full data-[starting-style]:-translate-x-full",
            className
          )}
        >
          {(title || description) && (
            <div className="border-b border-border px-5 py-4">
              {title && (
                <Dialog.Title className="text-base font-semibold text-foreground">
                  {title}
                </Dialog.Title>
              )}
              {description && (
                <Dialog.Description className="mt-0.5 text-sm text-muted-foreground">
                  {description}
                </Dialog.Description>
              )}
            </div>
          )}
          <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>
          {footer && <div className="border-t border-border px-5 py-4">{footer}</div>}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
