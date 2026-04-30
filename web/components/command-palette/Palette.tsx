"use client"

import {
  useState,
  useEffect,
  useRef,
  useCallback,
  useMemo,
} from "react"
import { useRouter } from "next/navigation"
import { useGlobalShortcut } from "@/hooks/useGlobalShortcut"
import {
  PAGE_ACTIONS,
  QUICK_ACTIONS,
  AI_ASK_ACTION,
  type PaletteAction,
} from "./groups"

// Characters required before AI mode is offered on Tab press.
const AI_TRIGGER_MIN_CHARS = 5

interface PaletteProps {
  /** Called when the user selects an AI query — opens the AI drawer. */
  onAIQuery?: (query: string) => void
}

/**
 * CommandPalette implements the ⌘K three-in-one panel:
 *   Search — filter pages and actions by label
 *   Action — quick navigation
 *   AI     — Tab to enter AI mode for queries longer than 5 chars
 *
 * Keyboard nav: arrow keys to move selection, Enter to activate,
 * Escape to close. Tab on a long query enters AI mode.
 */
export function CommandPalette({ onAIQuery }: PaletteProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [selectedIdx, setSelectedIdx] = useState(0)
  const [aiMode, setAiMode] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const router = useRouter()

  // Cmd+K opens palette.
  useGlobalShortcut({
    key: "k",
    onTrigger: () => {
      setOpen(true)
      setQuery("")
      setSelectedIdx(0)
      setAiMode(false)
    },
  })

  // Focus input when palette opens.
  useEffect(() => {
    if (open) setTimeout(() => inputRef.current?.focus(), 30)
  }, [open])

  const allItems = useMemo((): PaletteAction[] => {
    const base: PaletteAction[] = [...PAGE_ACTIONS, ...QUICK_ACTIONS]
    if (!query) return base

    const q = query.toLowerCase()
    const filtered = base.filter(
      (a) =>
        a.label.toLowerCase().includes(q) ||
        a.id.toLowerCase().includes(q)
    )

    // When query is long enough, prepend AI ask item at top.
    if (query.length >= AI_TRIGGER_MIN_CHARS) {
      return [AI_ASK_ACTION(query), ...filtered]
    }

    return filtered
  }, [query])

  const close = useCallback(() => {
    setOpen(false)
    setQuery("")
    setAiMode(false)
  }, [])

  const activate = useCallback(
    (item: PaletteAction) => {
      close()
      if (item.id === "ai-ask") {
        onAIQuery?.(query)
        return
      }
      if (item.href) {
        router.push(item.href)
      }
    },
    [close, onAIQuery, query, router]
  )

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault()
        setSelectedIdx((i) => Math.min(i + 1, allItems.length - 1))
        break
      case "ArrowUp":
        e.preventDefault()
        setSelectedIdx((i) => Math.max(i - 1, 0))
        break
      case "Enter":
        if (allItems[selectedIdx]) {
          activate(allItems[selectedIdx])
        }
        break
      case "Tab":
        // Tab on a long query enters AI mode.
        if (query.length >= AI_TRIGGER_MIN_CHARS && !aiMode) {
          e.preventDefault()
          setAiMode(true)
          setSelectedIdx(0)
        }
        break
      case "Escape":
        close()
        break
    }
  }

  // Group items for rendering.
  const groups = useMemo(() => {
    const g: Record<string, PaletteAction[]> = {}
    for (const item of allItems) {
      const key = item.id === "ai-ask" ? "ai" : item.group
      if (!g[key]) g[key] = []
      g[key].push(item)
    }
    return g
  }, [allItems])

  const groupLabels: Record<string, string> = {
    ai: "AI 模式",
    pages: "页面",
    actions: "操作",
    recent: "最近",
  }

  if (!open) return null

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-50 bg-black/30 backdrop-blur-sm"
        onClick={close}
        aria-hidden="true"
      />

      {/* Palette panel */}
      <div
        role="dialog"
        aria-label="命令面板"
        aria-modal="true"
        data-testid="command-palette"
        className="fixed left-1/2 top-[20vh] z-50 w-full max-w-lg -translate-x-1/2 overflow-hidden rounded-xl border border-border bg-background shadow-2xl"
      >
        {/* Search input */}
        <div className="flex items-center border-b border-border px-3">
          <span className="mr-2 text-muted-foreground" aria-hidden="true">
            {aiMode ? "✨" : "⌘"}
          </span>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setSelectedIdx(0)
              if (!e.target.value) setAiMode(false)
            }}
            onKeyDown={handleKeyDown}
            placeholder={aiMode ? "Ask AI…" : "搜索页面、操作…"}
            data-testid="palette-input"
            className="flex-1 bg-transparent py-3 text-sm text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
            aria-autocomplete="list"
            role="combobox"
            aria-expanded={true}
          />
          {query.length >= AI_TRIGGER_MIN_CHARS && !aiMode && (
            <span className="ml-2 rounded border border-border bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">
              Tab = AI
            </span>
          )}
          <kbd className="ml-2 rounded border border-border bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">
            ESC
          </kbd>
        </div>

        {/* Results */}
        <div
          className="max-h-80 overflow-y-auto py-1"
          role="listbox"
          aria-label="搜索结果"
        >
          {allItems.length === 0 && (
            <p className="px-4 py-8 text-center text-sm text-muted-foreground">
              没有匹配的结果
            </p>
          )}
          {(["ai", "recent", "pages", "actions"] as const).map((groupKey) => {
            const items = groups[groupKey]
            if (!items || items.length === 0) return null
            return (
              <div key={groupKey}>
                <div className="px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                  {groupLabels[groupKey] ?? groupKey}
                </div>
                {items.map((item) => {
                  const globalIdx = allItems.indexOf(item)
                  const isSelected = globalIdx === selectedIdx
                  return (
                    <button
                      key={item.id}
                      role="option"
                      aria-selected={isSelected}
                      onClick={() => activate(item)}
                      onMouseEnter={() => setSelectedIdx(globalIdx)}
                      data-testid={`palette-item-${item.id}`}
                      className={`flex w-full items-center gap-3 px-3 py-2 text-sm transition-colors ${
                        isSelected
                          ? "bg-primary/10 text-primary"
                          : "text-foreground hover:bg-muted"
                      } ${item.id === "ai-ask" ? "font-medium" : ""}`}
                    >
                      {item.icon && (
                        <span className="w-5 text-center text-base" aria-hidden="true">
                          {item.icon}
                        </span>
                      )}
                      <span className="flex-1 text-left">{item.label}</span>
                      {item.shortcut && (
                        <kbd className="rounded border border-border bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">
                          {item.shortcut}
                        </kbd>
                      )}
                    </button>
                  )
                })}
              </div>
            )
          })}
        </div>

        {/* Footer */}
        <div className="flex gap-3 border-t border-border px-3 py-2 text-[10px] text-muted-foreground">
          <span>↑↓ 导航</span>
          <span>↵ 确认</span>
          <span>Tab AI 模式</span>
          <span>Esc 关闭</span>
        </div>
      </div>
    </>
  )
}
