"use client"

import { useState, useRef, useEffect, useCallback } from "react"
import { motion, AnimatePresence, useReducedMotion } from "framer-motion"
import { SparklesIcon } from "lucide-react"
import { MessageList, type UIMessage } from "./MessageList"
import { streamChat, type AIPlan } from "@/lib/api/ai"
import { useGlobalShortcut } from "@/hooks/useGlobalShortcut"
import { useFocusTrap } from "@/hooks/useFocusTrap"
import { trackEvent } from "@/lib/telemetry"
import { Button } from "@/components/ui/button"

// Persisted conversation history key. Each tenant sees their own history because
// the key is stored per-browser tab; for a full multi-session experience a
// server-side store would be needed.
const STORAGE_KEY = "tally_ai_history"
const MAX_STORED_MESSAGES = 50

function loadHistory(): UIMessage[] {
  if (typeof window === "undefined") return []
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    return JSON.parse(raw) as UIMessage[]
  } catch {
    return []
  }
}

function saveHistory(msgs: UIMessage[]) {
  try {
    const trimmed = msgs.slice(-MAX_STORED_MESSAGES)
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmed))
  } catch {
    // ignore storage quota errors
  }
}

/**
 * AIDrawer is the right-side drawer for the AI inventory assistant.
 *
 * Opens via:
 *   - Floating action button (bottom-right ✨ icon)
 *   - Global shortcut Cmd+J / Ctrl+J
 *   - Programmatic: pass open=true and onClose
 *
 * Streaming: uses SSE via streamChat(). Each incoming chunk appends to the
 * current assistant message's content so the user sees a typewriter effect.
 */
export function AIDrawer() {
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<UIMessage[]>(loadHistory)
  const [input, setInput] = useState("")
  const [isLoading, setIsLoading] = useState(false)
  const [pendingAutoSend, setPendingAutoSend] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<(() => void) | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  // Records how the drawer was last opened so the ai_drawer_open telemetry can
  // attribute the trigger. Set by each opener before flipping `open` to true.
  const openTriggerRef = useRef<"shortcut" | "button" | "deeplink">("button")
  const reduceMotion = useReducedMotion()

  // Tab/Shift+Tab cycles within the panel while open — the drawer is a
  // hand-rolled aria-modal panel (not built on Base UI Dialog), so without
  // this Tab would leak focus onto the sidebar/page behind it.
  useFocusTrap(panelRef, open)

  // Cmd+J toggles drawer.
  useGlobalShortcut({
    key: "j",
    onTrigger: () =>
      setOpen((o) => {
        if (!o) openTriggerRef.current = "shortcut"
        return !o
      }),
  })

  // Listen for tally:ai-query events fired by the Command Palette.
  useEffect(() => {
    const handler = (e: Event) => {
      const query = (e as CustomEvent<{ query: string }>).detail?.query
      if (!query) return
      openTriggerRef.current = "deeplink"
      setOpen(true)
      setPendingAutoSend(query)
    }
    window.addEventListener("tally:ai-query", handler)
    return () => window.removeEventListener("tally:ai-query", handler)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Fire ai_drawer_open when the drawer transitions to open (H3 drawer DAU).
  useEffect(() => {
    if (open) {
      trackEvent("ai_drawer_open", {
        page_context: window.location.pathname,
        trigger: openTriggerRef.current,
      })
    }
  }, [open])

  // Auto-send when pendingAutoSend is set and the drawer has opened.
  useEffect(() => {
    if (!pendingAutoSend || !open || isLoading) return
    const q = pendingAutoSend
    setPendingAutoSend(null)
    setInput(q)
    // Use a timeout so React flushes the input state before we send.
    const timer = setTimeout(() => {
      // sendMessage reads input from state; by now the state should be q.
      // We duplicate the send logic inline to avoid stale-closure issues.
      setInput("")
      const history = messages
        .filter((m) => !m.isStreaming)
        .map((m) => ({ role: m.role, content: m.content }))
      setMessages((prev) => {
        const updated = [...prev, { role: "user" as const, content: q }]
        saveHistory(updated)
        return updated
      })
      setMessages((prev) => [
        ...prev,
        { role: "assistant" as const, content: "", isStreaming: true, plans: [] },
      ])
      setIsLoading(true)
      const cancel = streamChat(q, history, {
        onChunk: (chunk) => {
          setMessages((prev) => {
            const updated = [...prev]
            const last = updated[updated.length - 1]
            if (last?.role === "assistant" && last.isStreaming) {
              updated[updated.length - 1] = { ...last, content: last.content + chunk }
            }
            return updated
          })
        },
        onPlan: (plan) => {
          setMessages((prev) => {
            const updated = [...prev]
            const last = updated[updated.length - 1]
            if (last?.role === "assistant") {
              updated[updated.length - 1] = { ...last, plans: [...(last.plans ?? []), plan] }
            }
            return updated
          })
        },
        onDone: () => {
          setIsLoading(false)
          cancelRef.current = null
          setMessages((prev) => {
            const updated = [...prev]
            const last = updated[updated.length - 1]
            if (last?.role === "assistant" && last.isStreaming) {
              updated[updated.length - 1] = { ...last, isStreaming: false }
            }
            saveHistory(updated)
            return updated
          })
        },
        onError: (err) => {
          setIsLoading(false)
          cancelRef.current = null
          setMessages((prev) => {
            const updated = [...prev]
            const last = updated[updated.length - 1]
            if (last?.role === "assistant" && last.isStreaming) {
              updated[updated.length - 1] = {
                ...last,
                content: last.content || `错误: ${err}`,
                isStreaming: false,
              }
            }
            saveHistory(updated)
            return updated
          })
        },
      })
      cancelRef.current = cancel
    }, 150)
    return () => clearTimeout(timer)
  }, [pendingAutoSend, open, isLoading, messages]) // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-focus input when drawer opens.
  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 80)
    }
  }, [open])

  // Scroll to bottom on new messages.
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  const handleClose = useCallback(() => {
    setOpen(false)
    cancelRef.current?.()
  }, [])

  const sendMessage = useCallback((override?: string) => {
    // override lets a suggestion chip (or any programmatic trigger) send text
    // directly, without a race on the debounced input state.
    const text = (override ?? input).trim()
    if (!text || isLoading) return

    setInput("")

    const history = messages
      .filter((m) => !m.isStreaming)
      .map((m) => ({ role: m.role, content: m.content }))

    // Append user message.
    setMessages((prev) => {
      const updated = [...prev, { role: "user" as const, content: text }]
      saveHistory(updated)
      return updated
    })

    // Append empty assistant message that we'll fill via streaming.
    const assistantIdx = messages.length + 1 // position after the user message
    setMessages((prev) => [
      ...prev,
      { role: "assistant" as const, content: "", isStreaming: true, plans: [] },
    ])

    setIsLoading(true)
    const collectedPlans: AIPlan[] = []

    const cancel = streamChat(text, history, {
      onChunk: (chunk) => {
        setMessages((prev) => {
          const updated = [...prev]
          const last = updated[updated.length - 1]
          if (last?.role === "assistant" && last.isStreaming) {
            updated[updated.length - 1] = { ...last, content: last.content + chunk }
          }
          return updated
        })
      },
      onPlan: (plan) => {
        collectedPlans.push(plan)
        setMessages((prev) => {
          const updated = [...prev]
          const last = updated[updated.length - 1]
          if (last?.role === "assistant") {
            updated[updated.length - 1] = {
              ...last,
              plans: [...(last.plans ?? []), plan],
            }
          }
          return updated
        })
      },
      onDone: () => {
        setIsLoading(false)
        cancelRef.current = null
        setMessages((prev) => {
          const updated = [...prev]
          const last = updated[updated.length - 1]
          if (last?.role === "assistant" && last.isStreaming) {
            updated[updated.length - 1] = { ...last, isStreaming: false }
          }
          saveHistory(updated)
          return updated
        })
      },
      onError: (err) => {
        setIsLoading(false)
        cancelRef.current = null
        setMessages((prev) => {
          const updated = [...prev]
          const last = updated[updated.length - 1]
          if (last?.role === "assistant" && last.isStreaming) {
            updated[updated.length - 1] = {
              ...last,
              content: last.content || `错误: ${err}`,
              isStreaming: false,
            }
          }
          saveHistory(updated)
          return updated
        })
      },
    })

    cancelRef.current = cancel
    // assistantIdx used for reference only; streaming updates use prev tail.
    void assistantIdx
  }, [input, isLoading, messages])

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
    if (e.key === "Escape") {
      handleClose()
    }
  }

  const clearHistory = () => {
    setMessages([])
    window.localStorage.removeItem(STORAGE_KEY)
  }

  return (
    <>
      {/* Floating action button — a pill that reveals its label on hover; a
          soft ring hints it's the always-available AI entry point. Hidden while
          the drawer is open so it doesn't overlap the panel on narrow screens. */}
      <button
        onClick={() => {
          openTriggerRef.current = "button"
          setOpen(true)
        }}
        data-testid="ai-drawer-fab"
        aria-label="打开 AI 助手 (Cmd+J)"
        className={`group fixed bottom-5 right-5 z-40 flex h-12 items-center gap-2 rounded-full bg-primary pl-3.5 pr-3.5 text-primary-foreground shadow-lg ring-4 ring-primary/15 transition-all hover:scale-105 hover:pr-4 hover:shadow-xl hover:ring-primary/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${open ? "pointer-events-none scale-90 opacity-0" : "opacity-100"}`}
      >
        <SparklesIcon className="h-4 w-4 shrink-0" aria-hidden="true" />
        <span className="max-w-0 overflow-hidden whitespace-nowrap text-sm font-medium opacity-0 transition-all duration-200 group-hover:max-w-[5rem] group-hover:opacity-100">
          问 AI
        </span>
      </button>

      {/* Backdrop */}
      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: reduceMotion ? 0 : 0.2 }}
            className="fixed inset-0 z-40 bg-black/20 backdrop-blur-sm"
            onClick={handleClose}
            aria-hidden="true"
          />
        )}
      </AnimatePresence>

      {/* Drawer panel (always mounted; slides via x) */}
      <motion.div
        ref={panelRef}
        data-testid="ai-drawer"
        role="dialog"
        aria-label="AI 助手"
        aria-modal="true"
        initial={false}
        animate={{ x: open ? 0 : "100%" }}
        transition={reduceMotion ? { duration: 0 } : { type: "spring", stiffness: 380, damping: 40 }}
        className="fixed right-0 top-0 z-50 flex h-full w-full max-w-sm flex-col bg-background shadow-2xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <SparklesIcon className="h-4 w-4 shrink-0" aria-hidden="true" />
            <h2 className="text-sm font-semibold">AI 助手</h2>
            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              Cmd+J
            </span>
          </div>
          <div className="flex items-center gap-1">
            {messages.length > 0 && (
              <button
                onClick={clearHistory}
                className="rounded px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
                title="清空历史"
              >
                清空
              </button>
            )}
            <button
              onClick={handleClose}
              aria-label="关闭"
              className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              ✕
            </button>
          </div>
        </div>

        {/* Message list */}
        <div className="flex-1 overflow-y-auto">
          <MessageList messages={messages} onSuggestion={(q) => sendMessage(q)} />
          <div ref={messagesEndRef} />
        </div>

        {/* Input */}
        <div className="border-t border-border p-3">
          {isLoading && (
            <div className="mb-2 flex items-center gap-1.5 text-xs text-muted-foreground" role="status" aria-live="polite">
              <span className="inline-flex gap-1" aria-hidden="true">
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary [animation-delay:-0.3s]" />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary [animation-delay:-0.15s]" />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary" />
              </span>
              正在查你的库存…
            </div>
          )}
          <div className="flex gap-2">
            <input
              ref={inputRef}
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="问我关于库存、销售的问题..."
              disabled={isLoading}
              data-testid="ai-input"
              className="flex-1 rounded-lg border border-border bg-muted/50 px-3 py-2 text-sm transition-colors placeholder:text-muted-foreground/60 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus:outline-none disabled:opacity-50"
            />
            <Button
              onClick={() => sendMessage()}
              disabled={isLoading || !input.trim()}
              data-testid="ai-send-btn"
              size="lg"
            >
              发送
            </Button>
          </div>
        </div>
      </motion.div>
    </>
  )
}
