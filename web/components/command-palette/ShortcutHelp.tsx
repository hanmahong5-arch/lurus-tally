"use client"

import { useEffect, useState } from "react"

import { Modal } from "@/components/ui/modal"

const SHORTCUTS: { keys: string; label: string; ai?: boolean }[] = [
  { keys: "⌘K / Ctrl+K", label: "命令面板（搜索页面 / 操作 / AI）", ai: true },
  { keys: "⌘J / Ctrl+J", label: "AI 助手抽屉", ai: true },
  { keys: "⌘Z / Ctrl+Z", label: "撤销上一步删除 / 作废" },
  { keys: "Tab", label: "命令面板内进入 AI 模式（≥5 字）", ai: true },
  { keys: "?", label: "打开本快捷键帮助" },
  { keys: "Esc", label: "关闭面板 / 抽屉 / 弹窗" },
]

// Rows that mention the assistant read differently when the shop has none.
const NO_AI_OVERRIDES: Record<string, { keys: string; label: string } | null> = {
  "⌘K / Ctrl+K": { keys: "⌘K / Ctrl+K", label: "命令面板（搜索页面 / 操作）" },
  "⌘J / Ctrl+J": null,
  Tab: null,
}

/**
 * ShortcutHelp surfaces the global keyboard map. Opened by pressing `?`
 * (Shift+/) anywhere outside a text field — useGlobalShortcut only handles
 * meta/ctrl combos, so this binds its own guarded listener.
 */
export function ShortcutHelp({ aiEnabled = true }: { aiEnabled?: boolean } = {}) {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key !== "?" || e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      ) {
        return
      }
      e.preventDefault()
      setOpen(true)
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [])

  const rows = aiEnabled
    ? SHORTCUTS
    : SHORTCUTS.flatMap((s) => {
        if (!s.ai) return [s]
        const o = NO_AI_OVERRIDES[s.keys]
        return o ? [o] : []
      })

  return (
    <Modal open={open} onOpenChange={setOpen} title="键盘快捷键">
      <ul className="mt-3 flex flex-col gap-2">
        {rows.map((s) => (
          <li key={s.keys} className="flex items-center justify-between gap-4 text-sm">
            <span className="text-muted-foreground">{s.label}</span>
            <kbd className="shrink-0 rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
              {s.keys}
            </kbd>
          </li>
        ))}
      </ul>
    </Modal>
  )
}
