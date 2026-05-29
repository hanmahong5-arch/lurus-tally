/**
 * AI assistant API client for Tally.
 *
 * POST /api/proxy/ai/chat     — SSE streaming endpoint (raw fetch + ReadableStream)
 * POST /api/proxy/ai/plans/:id/confirm — apiFetch (auto Idempotency-Key)
 * POST /api/proxy/ai/plans/:id/cancel  — apiFetch (auto Idempotency-Key)
 *
 * SSE intentionally bypasses apiFetch: it needs raw stream access, a long-lived
 * AbortController, and per-event error semantics that the JSON fast path cannot
 * model. The plan confirm/cancel writes go through apiFetch so they pick up the
 * shared idempotency / retry / offline / toast layer.
 */

import { apiFetch } from "./client"

const BASE = "/api/proxy"

// ChatMessage is a single turn in the conversation history.
export interface ChatMessage {
  role: "user" | "assistant" | "tool"
  content: string
}

// PlanSampleRow is one row in a plan preview.
export interface PlanSampleRow {
  name: string
  before: string
  after: string
}

// PlanPreview contains the data for the plan confirmation card.
export interface PlanPreview {
  description: string
  affected_count: number
  sample_rows: PlanSampleRow[]
}

// AIPlan is a destructive operation awaiting user confirmation.
export interface AIPlan {
  id: string
  tenant_id: string
  type: string
  status: "pending" | "confirmed" | "cancelled" | "expired" | "failed"
  payload: Record<string, unknown>
  preview: PlanPreview
  created_at: string
  expires_at: string
  // trace_id is the 32-hex LLM trace identifier; present only when the
  // observability backend is configured. PlanCard renders it as a deep link to
  // <NEXT_PUBLIC_LANGFUSE_HOST>/trace/<trace_id>.
  trace_id?: string
}

// ConfirmPlanResult is the response of a successful plan confirm. When the plan
// produced a purchase draft, bill_id/bill_no let the UI deep-link to it.
export interface ConfirmPlanResult {
  plan_id: string
  status: string
  type: string
  affected_count?: number
  bill_id?: string
  bill_no?: string
}

// SSEEventType names the possible event types in the AI chat SSE stream.
export type SSEEventType = "chunk" | "plan" | "done" | "error"

// SSEEvent is a parsed event from the SSE stream.
export interface SSEEvent {
  type: SSEEventType
  data: unknown
}

/**
 * streamChat opens an SSE connection to the AI chat endpoint.
 * Calls onChunk for each streamed text chunk, onPlan for each plan card, and
 * onDone/onError when the stream terminates.
 *
 * Returns a cancel function that aborts the request.
 */
export function streamChat(
  message: string,
  history: ChatMessage[],
  callbacks: {
    onChunk: (content: string) => void
    onPlan: (plan: AIPlan) => void
    onDone: () => void
    onError: (err: string) => void
  }
): () => void {
  const controller = new AbortController()

  const run = async () => {
    let resp: Response
    try {
      resp = await fetch(`${BASE}/ai/chat`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message, history }),
        signal: controller.signal,
      })
    } catch (err: unknown) {
      if ((err as Error)?.name !== "AbortError") {
        callbacks.onError(String(err))
      }
      return
    }

    if (!resp.ok) {
      const body = await resp.json().catch(() => ({})) as { error?: string; detail?: string }
      callbacks.onError(body.detail ?? body.error ?? `HTTP ${resp.status}`)
      return
    }

    if (!resp.body) {
      callbacks.onError("no response body")
      return
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ""

    const processLine = (line: string) => {
      // ignore empty lines and comment lines
      if (!line || line.startsWith(":")) return null
      if (line.startsWith("data: ")) return { field: "data", value: line.slice(6) }
      if (line.startsWith("event: ")) return { field: "event", value: line.slice(7) }
      return null
    }

    let currentEvent = ""
    let currentData = ""

    const dispatchEvent = () => {
      if (!currentData && !currentEvent) return
      const type = (currentEvent || "message") as SSEEventType
      try {
        const parsed: unknown = JSON.parse(currentData)
        switch (type) {
          case "chunk": {
            const c = parsed as { content?: string }
            if (c.content) callbacks.onChunk(c.content)
            break
          }
          case "plan":
            callbacks.onPlan(parsed as AIPlan)
            break
          case "done":
            callbacks.onDone()
            break
          case "error": {
            const e = parsed as { error?: string }
            callbacks.onError(e.error ?? "unknown error")
            break
          }
        }
      } catch {
        // ignore malformed events
      }
      currentEvent = ""
      currentData = ""
    }

    try {
      // eslint-disable-next-line no-constant-condition
      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split("\n")
        buffer = lines.pop() ?? ""

        for (const line of lines) {
          const result = processLine(line)
          if (result === null) {
            // blank line → dispatch accumulated event
            if (line === "") {
              dispatchEvent()
            }
          } else if (result.field === "event") {
            currentEvent = result.value
          } else if (result.field === "data") {
            currentData = result.value
          }
        }
      }
      // process any remaining
      if (buffer) {
        processLine(buffer)
        dispatchEvent()
      }
    } catch (err: unknown) {
      if ((err as Error)?.name !== "AbortError") {
        callbacks.onError(String(err))
      }
    }
  }

  run()
  return () => controller.abort()
}

/**
 * confirmPlan sends a confirm request for a pending plan.
 *
 * Goes through apiFetch so the Idempotency-Key middleware on the backend can
 * dedupe accidental double submits (rapid double-click, network retry, etc).
 * `silent: true` keeps the toast UX in the caller's hands — PlanCard renders
 * its own inline error.
 */
export async function confirmPlan(planId: string): Promise<ConfirmPlanResult> {
  return callPlanAction<ConfirmPlanResult>(planId, "confirm")
}

/**
 * cancelPlan sends a cancel request for a pending plan.
 */
export async function cancelPlan(planId: string): Promise<void> {
  await callPlanAction<unknown>(planId, "cancel")
}

// RevertPlanResult is the response of a successful plan revert.
export interface RevertPlanResult {
  plan_id: string
  reverted_type: string
  affected_count: number
}

/**
 * revertPlan asks the server to undo the side effects of a confirmed plan.
 * Only bulk_stock_adjust and price_change types are supported server-side.
 * Must be called within 30 seconds of confirmation.
 */
export async function revertPlan(planId: string): Promise<RevertPlanResult> {
  return callPlanAction<RevertPlanResult>(planId, "revert")
}

async function callPlanAction<T>(planId: string, action: "confirm" | "cancel" | "revert"): Promise<T> {
  // Re-throw ApiError directly so callers (e.g. PlanCard) can inspect .status
  // and .code for fine-grained UX (e.g. 422 execution_failed → failed visual).
  return apiFetch<T>(`${BASE}/ai/plans/${planId}/${action}`, {
    method: "POST",
    silent: true,
  })
}
