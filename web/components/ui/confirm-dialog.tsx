"use client"

import { Button } from "@/components/ui/button"
import { Modal } from "@/components/ui/modal"

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
 * Promise wiring and keeps one dialog instance per provider tree. It now renders
 * through Modal, so it inherits the shared backdrop + fade/scale transition.
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
    <Modal
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel()
      }}
      title={title}
      description={body}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault()
          onConfirm()
        }
      }}
      footer={
        <>
          <Button variant="outline" size="sm" onClick={onCancel}>
            {cancelText}
          </Button>
          <Button
            variant={danger ? "destructive" : "default"}
            size="sm"
            autoFocus
            onClick={onConfirm}
          >
            {confirmText}
          </Button>
        </>
      }
    />
  )
}
