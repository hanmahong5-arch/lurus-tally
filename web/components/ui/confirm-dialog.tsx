"use client"

import { Dialog } from "@base-ui/react/dialog"
import { cn } from "@/lib/utils"

export interface ConfirmDialogOptions {
  title?: string
  body?: string
  confirmText?: string
  cancelText?: string
  /** Render the confirm button in destructive styling. */
  danger?: boolean
}

interface ConfirmDialogProps extends ConfirmDialogOptions {
  open: boolean
  onConfirm: () => void
  onCancel: () => void
}

/**
 * ConfirmDialog is the project's single confirmation primitive. Always reach for
 * `useConfirm()` rather than mounting this directly — the hook handles the
 * Promise wiring and keeps one dialog instance per provider tree.
 */
export function ConfirmDialog({
  open,
  title = "确认操作",
  body,
  confirmText = "确认",
  cancelText = "取消",
  danger = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/40 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <Dialog.Popup
          className={cn(
            "fixed left-1/2 top-1/2 z-50 w-[92vw] max-w-md -translate-x-1/2 -translate-y-1/2",
            "rounded-lg border border-border bg-background p-5 shadow-xl outline-none",
          )}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault()
              onConfirm()
            }
          }}
        >
          <Dialog.Title className="text-base font-semibold text-foreground">
            {title}
          </Dialog.Title>
          {body && (
            <Dialog.Description className="mt-2 text-sm text-muted-foreground">
              {body}
            </Dialog.Description>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={onCancel}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-sm hover:bg-muted transition-colors disabled:opacity-50"
            >
              {cancelText}
            </button>
            <button
              type="button"
              autoFocus
              onClick={onConfirm}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-50",
                danger
                  ? "bg-destructive text-destructive-foreground hover:bg-destructive/90"
                  : "bg-primary text-primary-foreground hover:bg-primary/90",
              )}
            >
              {confirmText}
            </button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
