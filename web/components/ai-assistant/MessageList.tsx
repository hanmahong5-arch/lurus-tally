"use client"

import { PlanCard } from "./PlanCard"
import { AssistantContent } from "./AssistantContent"
import type { AIPlan, ChatMessage } from "@/lib/api/ai"

export interface UIMessage extends ChatMessage {
  // plans is only present on assistant messages that triggered plan creation
  plans?: AIPlan[]
  // isStreaming indicates this message is still being written
  isStreaming?: boolean
}

interface MessageListProps {
  messages: UIMessage[]
  onPlanConfirmed?: (planId: string) => void
  onPlanCancelled?: (planId: string) => void
  // onSuggestion fires when the user taps a starter-prompt chip in the empty
  // state. The drawer sends it as if the user had typed and pressed enter.
  onSuggestion?: (query: string) => void
}

// Starter prompts shown in the empty state. Chosen to showcase the assistant's
// read-only intelligence (low-stock / velocity / dead-stock / margin) so a
// first-time user sees, in one glance, the natural-language questions Tally can
// answer about their own inventory — no manual query building.
const STARTER_PROMPTS = [
  { icon: "📉", label: "哪些商品快断货了?" },
  { icon: "🔥", label: "这个月卖得最好的 5 个商品" },
  { icon: "🧊", label: "帮我看看有哪些呆滞库存" },
  { icon: "💰", label: "毛利最低的商品有哪些?" },
]

/**
 * MessageList renders the full conversation history.
 *
 * User messages appear on the right; assistant messages on the left.
 * Assistant messages may include plan cards that require user confirmation.
 * The typing cursor (▋) is shown when isStreaming is true.
 */
export function MessageList({ messages, onPlanConfirmed, onPlanCancelled, onSuggestion }: MessageListProps) {
  if (messages.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center px-5 py-8">
        <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 text-3xl">
          ✨
        </div>
        <h3 className="mt-4 text-center text-base font-semibold text-foreground">
          问我关于你库存的任何问题
        </h3>
        <p className="mt-1 text-center text-xs text-muted-foreground">
          用大白话提问 · 数据只读不改
        </p>
        <div className="mt-6 grid w-full max-w-xs gap-2">
          {STARTER_PROMPTS.map((p) => (
            <button
              key={p.label}
              type="button"
              onClick={() => onSuggestion?.(p.label)}
              disabled={!onSuggestion}
              data-testid="ai-suggestion"
              className="group flex items-center gap-3 rounded-xl border border-border bg-card px-3.5 py-2.5 text-left text-sm text-foreground shadow-sm transition-all hover:-translate-y-0.5 hover:border-primary/40 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50"
            >
              <span aria-hidden="true" className="text-base">{p.icon}</span>
              <span className="flex-1 leading-snug">{p.label}</span>
              <span
                aria-hidden="true"
                className="text-muted-foreground/40 transition-transform group-hover:translate-x-0.5 group-hover:text-primary"
              >
                →
              </span>
            </button>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3 p-3">
      {messages.map((msg, idx) => (
        <div key={idx} className={`flex ${msg.role === "user" ? "justify-end" : "justify-start"}`}>
          <div
            className={`rounded-2xl px-3 py-2 text-sm leading-relaxed ${
              msg.role === "user"
                ? "max-w-[85%] bg-primary text-primary-foreground"
                : "w-full bg-muted/60 text-foreground"
            }`}
          >
            {/* Assistant: rich result blocks once finished (tables → result
                cards, bold figures, lists); raw text + cursor while streaming so
                the typewriter effect stays smooth. User: plain text. */}
            {msg.role === "assistant" && !msg.isStreaming && msg.content ? (
              <AssistantContent text={msg.content} />
            ) : (
              <p className="whitespace-pre-wrap break-words">
                {msg.content}
                {msg.isStreaming && (
                  <span className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse rounded-sm bg-primary align-middle" />
                )}
              </p>
            )}

            {/* Plan cards */}
            {msg.plans && msg.plans.length > 0 && (
              <div className="mt-2">
                {msg.plans.map((plan) => (
                  <PlanCard
                    key={plan.id}
                    plan={plan}
                    onConfirmed={() => onPlanConfirmed?.(plan.id)}
                    onCancelled={() => onPlanCancelled?.(plan.id)}
                  />
                ))}
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}
