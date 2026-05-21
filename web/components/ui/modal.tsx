"use client"

import type { KeyboardEventHandler, ReactNode } from "react"
import { Dialog } from "@base-ui/react/dialog"

import { cn } from "@/lib/utils"

export interface ModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title?: ReactNode
  description?: ReactNode
  children?: ReactNode
  /** Right-aligned footer row (actions). */
  footer?: ReactNode
  className?: string
  onKeyDown?: KeyboardEventHandler<HTMLDivElement>
}

/**
 * Modal is the centred dialog shell — generalised from the old ConfirmDialog so
 * every modal shares one radius, backdrop, and enter/exit transition. The
 * fade + scale runs on Base UI's `data-starting/ending-style` attributes, so it
 * is real (the previous `animate-in` classes were dead) yet plugin-free and
 * disabled automatically under `prefers-reduced-motion`.
 */
export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  className,
  onKeyDown,
}: ModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/50 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup
          onKeyDown={onKeyDown}
          className={cn(
            "fixed left-1/2 top-1/2 z-50 w-[92vw] max-w-md -translate-x-1/2 -translate-y-1/2",
            "rounded-xl border border-border bg-background p-5 shadow-xl outline-none",
            "transition-all duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0",
            className
          )}
        >
          {title && (
            <Dialog.Title className="text-base font-semibold text-foreground">
              {title}
            </Dialog.Title>
          )}
          {description && (
            <Dialog.Description className="mt-2 text-sm text-muted-foreground">
              {description}
            </Dialog.Description>
          )}
          {children}
          {footer && <div className="mt-5 flex justify-end gap-2">{footer}</div>}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
