"use client"

import { createContext, createElement, useCallback, useContext, useMemo, useRef, useState } from "react"
import { ConfirmDialog, type ConfirmDialogOptions } from "@/components/ui/confirm-dialog"

type ConfirmFn = (options: ConfirmDialogOptions) => Promise<boolean>

const ConfirmContext = createContext<ConfirmFn | null>(null)

interface DialogState extends ConfirmDialogOptions {
  open: boolean
}

/**
 * ConfirmProvider mounts a single shared ConfirmDialog instance and exposes
 * an async `confirm()` via context. Wrap once near the root of the app.
 */
export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<DialogState>({ open: false })
  const resolverRef = useRef<((value: boolean) => void) | null>(null)

  const confirm = useCallback<ConfirmFn>((options) => {
    return new Promise<boolean>((resolve) => {
      resolverRef.current = resolve
      setState({ ...options, open: true })
    })
  }, [])

  const resolve = useCallback((value: boolean) => {
    setState((s) => ({ ...s, open: false }))
    const r = resolverRef.current
    resolverRef.current = null
    r?.(value)
  }, [])

  const value = useMemo(() => confirm, [confirm])

  return createElement(
    ConfirmContext.Provider,
    { value },
    children,
    createElement(ConfirmDialog, {
      open: state.open,
      title: state.title,
      body: state.body,
      confirmText: state.confirmText,
      cancelText: state.cancelText,
      danger: state.danger,
      onConfirm: () => resolve(true),
      onCancel: () => resolve(false),
    }),
  )
}

/**
 * useConfirm returns a Promise-based confirm() — replaces window.confirm.
 *
 *   const confirm = useConfirm()
 *   if (await confirm({ title: "确认删除", danger: true })) { ... }
 *
 * Confirmation strategy (keep consistent across the app):
 *   - Reversible actions (delete-with-restore, void-with-restore) → DO NOT
 *     prompt. Act immediately and offer an undo toast / Cmd+Z. Adding a dialog
 *     to an undoable delete just adds friction.
 *   - Irreversible actions (approve a bill, revoke a session, delete an API
 *     key) → confirm({ danger: true }) so the user can't fat-finger them.
 */
export function useConfirm(): ConfirmFn {
  const ctx = useContext(ConfirmContext)
  if (!ctx) {
    throw new Error("useConfirm must be used inside <ConfirmProvider>")
  }
  return ctx
}
