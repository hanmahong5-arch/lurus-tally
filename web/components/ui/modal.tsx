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
  /** 小屏（<md）全屏显示：内容可滚动，关闭按钮固定在顶部；桌面仍是居中对话框。默认关闭。 */
  fullscreenOnMobile?: boolean
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
  fullscreenOnMobile = false,
  onKeyDown,
}: ModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/50 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup
          onKeyDown={onKeyDown}
          className={cn(
            fullscreenOnMobile
              ? [
                  "fixed inset-0 z-50 h-[100dvh] w-screen max-w-none overflow-y-auto overscroll-contain",
                  "md:inset-auto md:left-1/2 md:top-1/2 md:h-auto md:max-h-[90vh] md:w-[92vw] md:max-w-md md:-translate-x-1/2 md:-translate-y-1/2",
                  "border-border bg-background p-5 shadow-xl outline-none md:rounded-xl md:border",
                ]
              : [
                  "fixed left-1/2 top-1/2 z-50 w-[92vw] max-w-md -translate-x-1/2 -translate-y-1/2",
                  "rounded-xl border border-border bg-background p-5 shadow-xl outline-none",
                ],
            "transition-all duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0",
            className
          )}
        >
          {fullscreenOnMobile && (
            <div className="sticky top-0 z-10 -mx-5 -mt-5 mb-2 flex justify-end bg-background/95 px-3 py-2 md:hidden">
              <Dialog.Close
                aria-label="关闭"
                className="inline-flex min-h-[44px] min-w-[44px] items-center justify-center rounded-md text-lg text-muted-foreground hover:bg-muted"
              >
                ✕
              </Dialog.Close>
            </div>
          )}
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
