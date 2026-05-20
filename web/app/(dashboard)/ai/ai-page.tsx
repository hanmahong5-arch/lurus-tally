"use client"

import { useCallback, useEffect, useRef, useState } from "react"

import { MessageList, type UIMessage } from "@/components/ai-assistant/MessageList"
import { streamChat, type AIPlan } from "@/lib/api/ai"

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
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(msgs.slice(-MAX_STORED_MESSAGES)))
  } catch {
    // quota / disabled — silently ignore
  }
}

const PROMPT_SUGGESTIONS = [
  "本月销售额是多少？",
  "哪些商品库存不足？",
  "上周成交最多的客户是谁？",
  "帮我把所有低库存商品降价 5%",
]

/**
 * Full-page AI assistant. Mirrors the conversation state of the AIDrawer
 * (same localStorage key) so opening this page picks up where the drawer
 * left off, and vice versa.
 *
 * Layout: header strip + scrollable message list + suggestions + composer.
 * Streaming uses streamChat() with onChunk / onPlan / onDone / onError.
 */
export function AIPage() {
  const [messages, setMessages] = useState<UIMessage[]>(loadHistory)
  const [input, setInput] = useState("")
  const [isLoading, setIsLoading] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const cancelRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const send = useCallback(
    (text: string) => {
      const q = text.trim()
      if (!q || isLoading) return

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
        { role: "assistant" as const, content: "", isStreaming: true, plans: [] as AIPlan[] },
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
    },
    [isLoading, messages],
  )

  function handleKey(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      send(input)
    }
  }

  function clear() {
    cancelRef.current?.()
    setMessages([])
    window.localStorage.removeItem(STORAGE_KEY)
  }

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between border-b border-border px-6 py-4">
        <div>
          <h1 className="text-xl font-semibold">AI 助手</h1>
          <p className="text-xs text-muted-foreground">
            自然语言问数 + 库存补货 / 调价计划。修改类操作会在执行前出确认卡。
          </p>
        </div>
        {messages.length > 0 && (
          <button
            type="button"
            onClick={clear}
            className="rounded-md border border-border bg-background px-3 py-1.5 text-xs hover:bg-muted"
          >
            清空对话
          </button>
        )}
      </header>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl">
          <MessageList messages={messages} />
          <div ref={messagesEndRef} />
        </div>
      </div>

      {messages.length === 0 && (
        <div className="mx-auto w-full max-w-3xl px-4 pb-2">
          <p className="mb-2 text-xs text-muted-foreground">试试这些问题：</p>
          <div className="flex flex-wrap gap-2">
            {PROMPT_SUGGESTIONS.map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => send(p)}
                className="rounded-full border border-border bg-card px-3 py-1.5 text-xs hover:bg-muted"
              >
                {p}
              </button>
            ))}
          </div>
        </div>
      )}

      <div className="border-t border-border bg-background px-4 py-3">
        <div className="mx-auto flex max-w-3xl items-end gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKey}
            rows={1}
            placeholder="问我关于库存、销售、客户的问题..."
            disabled={isLoading}
            className="flex-1 resize-none rounded-lg border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground/60 focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
          />
          <button
            type="button"
            onClick={() => send(input)}
            disabled={isLoading || !input.trim()}
            className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {isLoading ? "..." : "发送"}
          </button>
        </div>
      </div>
    </div>
  )
}
