"use client"

import { PlanCard } from "./PlanCard"
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
}

/**
 * MessageList renders the full conversation history.
 *
 * User messages appear on the right; assistant messages on the left.
 * Assistant messages may include plan cards that require user confirmation.
 * The typing cursor (▋) is shown when isStreaming is true.
 */
export function MessageList({ messages, onPlanConfirmed, onPlanCancelled }: MessageListProps) {
  if (messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        <div className="text-center">
          <p className="text-2xl mb-2">✨</p>
          <p>问我关于库存、销售、定价的任何问题</p>
          <p className="mt-1 text-xs opacity-70">例如：&ldquo;低库存商品有哪些？&rdquo;</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3 p-3">
      {messages.map((msg, idx) => (
        <div key={idx} className={`flex ${msg.role === "user" ? "justify-end" : "justify-start"}`}>
          <div
            className={`max-w-[85%] rounded-2xl px-3 py-2 text-sm leading-relaxed ${
              msg.role === "user"
                ? "bg-primary text-primary-foreground"
                : "bg-muted text-foreground"
            }`}
          >
            {/* Message text — preserve newlines */}
            <p className="whitespace-pre-wrap break-words">
              {msg.content}
              {msg.isStreaming && (
                <span className="animate-pulse">▋</span>
              )}
            </p>

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
