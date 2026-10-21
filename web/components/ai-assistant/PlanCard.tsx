"use client"

import { useRef, useState } from "react"
import Link from "next/link"
import { type AIPlan, type ConfirmPlanResult, confirmPlan, cancelPlan, revertPlan } from "@/lib/api/ai"
import { ApiError, extractApiError } from "@/lib/api/errors"
import { cancelPurchaseBill } from "@/lib/api/purchase"
import { globalUndoStack } from "@/lib/undo/undo-stack"
import { trackEvent } from "@/lib/telemetry"
import { ErrorBanner } from "@/components/ui/error-banner"

// planKind maps a backend plan type to the bounded telemetry "kind" enum used
// by the plan_accept_rate metric (drives Kill-switch #2: AI-PO order rate).
function planKind(type: string): "replenishment" | "movement" | "transfer" | "other" {
  switch (type) {
    case "create_purchase_draft":
      return "replenishment"
    case "bulk_stock_adjust":
      return "movement"
    default:
      return "other"
  }
}

// DraftFlag is one deterministic-rule finding the backend attached to a draft
// (quantity differs from what was said, far above recent orders, price far from
// last time, lines not adding up to the stated total). `line` is the 0-based
// sample row, or -1 for the whole document.
interface DraftFlag {
  line: number
  code: string
  message: string
}

function parseFlags(payload: AIPlan["payload"] | undefined): DraftFlag[] {
  const raw = payload?.flags
  if (!Array.isArray(raw)) return []
  return raw.filter(
    (f): f is DraftFlag =>
      !!f && typeof f === "object" && typeof (f as DraftFlag).line === "number" && typeof (f as DraftFlag).message === "string",
  )
}

interface PlanCardProps {
  plan: AIPlan
  onConfirmed?: () => void
  onCancelled?: () => void
}

/**
 * PlanCard renders a destructive-operation confirmation card.
 *
 * The model returns a plan (not an execution result). The user must explicitly
 * click "Confirm" to execute, or "Cancel" to dismiss. This is the safety gate
 * that prevents accidental bulk operations.
 */
export function PlanCard({ plan, onConfirmed, onCancelled }: PlanCardProps) {
  const [status, setStatus] = useState<"idle" | "confirming" | "cancelling">("idle")
  const [outcome, setOutcome] = useState<"confirmed" | "cancelled" | "failed" | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<ConfirmPlanResult | null>(null)
  // Synchronous in-flight latch — prevents the extra-fast double click that fires
  // before React commits the "confirming" state and toggles the disabled prop.
  const inFlightRef = useRef(false)
  // Flagged drafts need every flagged line ticked before "确认执行" unlocks.
  const flags = parseFlags(plan.payload)
  const [ticked, setTicked] = useState<Record<number, boolean>>({})
  const flaggedLines = new Set(flags.filter((f) => f.line >= 0).map((f) => f.line))
  const allTicked = flags.every((_, i) => ticked[i])

  // handleDismissFailed is used in the failed terminal branch to call cancelPlan
  // and remove the plan from the list (same semantics as handleCancel, but
  // defined before the early-return so it closes over state correctly).
  const handleDismissFailed = async () => {
    if (inFlightRef.current) return
    inFlightRef.current = true
    try {
      await cancelPlan(plan.id)
      onCancelled?.()
    } catch {
      // Best-effort dismiss: the plan is already in a terminal state, so a
      // cancel failure only affects list cleanup, not data integrity.
    } finally {
      inFlightRef.current = false
    }
  }

  const settled = outcome
    ?? (plan.status === "confirmed" ? "confirmed"
      : plan.status === "failed" ? "failed"
      : plan.status !== "pending" ? "cancelled"
      : null)
  if (settled) {
    if (settled === "failed") {
      return (
        <div
          className="my-2 rounded-lg border border-warning/40 bg-warning/5 p-3 text-sm"
          data-testid="plan-card-failed"
        >
          <p className="font-medium text-warning">✗ 执行失败</p>
          {error && <p className="mt-1 text-muted-foreground">{error}</p>}
          <p className="mt-1 text-xs text-muted-foreground">建议联系运营或重新发起新的建议</p>
          <div className="mt-2">
            <button
              onClick={handleDismissFailed}
              data-testid="plan-cancel-btn"
              className="rounded border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted"
            >
              关闭
            </button>
          </div>
        </div>
      )
    }
    if (settled === "cancelled") {
      return (
        <div className="my-2 rounded-lg border border-border bg-muted/50 p-3 text-sm text-muted-foreground">
          ✗ 操作已取消
        </div>
      )
    }
    return (
      <div className="my-2 rounded-lg border border-success/40 bg-success/5 p-3 text-sm" data-testid="plan-card-done">
        <p className="text-foreground">✓ 操作已执行{result?.affected_count != null ? `（影响 ${result.affected_count} 条）` : ""}</p>
        {result?.bill_id && (
          <Link
            href={`/purchases/${result.bill_id}`}
            data-testid="plan-bill-link"
            className="mt-1 inline-block font-medium text-success hover:underline"
          >
            已建采购草稿 {result.bill_no ? `${result.bill_no} ` : ""}→ 查看
          </Link>
        )}
      </div>
    )
  }

  const handleConfirm = async () => {
    if (!allTicked) return
    if (inFlightRef.current) return
    inFlightRef.current = true
    setStatus("confirming")
    setError(null)
    try {
      const acknowledged = flags.map((_, i) => i).filter((i) => ticked[i])
      const res = await confirmPlan(plan.id, acknowledged)
      setResult(res)
      setOutcome("confirmed")
      trackEvent("plan_accept_rate", { plan_id: plan.id, kind: planKind(plan.type), accepted: "1" })
      // Make the AI write reversible within 30 s (Cmd+Z / toast).
      // Each plan type has its own undo path:
      //   purchase draft  → cancel the bill client-side (no server undo needed)
      //   stock adjust    → POST /plans/:id/revert (server reverses movements)
      //   price change    → POST /plans/:id/revert (server restores from snapshot)
      if (res.bill_id) {
        const billId = res.bill_id
        globalUndoStack.push({
          type: "ai_purchase_draft",
          id: billId,
          billNo: res.bill_no ?? "",
          revert: async () => {
            await cancelPurchaseBill(billId)
          },
        })
      } else if (plan.type === "bulk_stock_adjust") {
        const planId = plan.id
        globalUndoStack.push({
          type: "ai_stock_adjust",
          planId,
          affectedCount: res.affected_count ?? 0,
          revert: async () => {
            await revertPlan(planId)
          },
        })
      } else if (plan.type === "price_change") {
        const planId = plan.id
        globalUndoStack.push({
          type: "ai_price_change",
          planId,
          affectedCount: res.affected_count ?? 0,
          revert: async () => {
            await revertPlan(planId)
          },
        })
      }
      onConfirmed?.()
    } catch (err: unknown) {
      // 422 execution_failed → transition to failed terminal state so users can
      // clearly distinguish "system error during execution" from "user cancelled".
      if (err instanceof ApiError && err.status === 422 && err.code === "execution_failed") {
        setOutcome("failed")
        setError(err.message)
      } else {
        setError(extractApiError(err).message)
      }
      setStatus("idle")
    } finally {
      inFlightRef.current = false
    }
  }

  const handleCancel = async () => {
    if (inFlightRef.current) return
    inFlightRef.current = true
    setStatus("cancelling")
    setError(null)
    try {
      await cancelPlan(plan.id)
      setOutcome("cancelled")
      trackEvent("plan_accept_rate", { plan_id: plan.id, kind: planKind(plan.type), accepted: "0" })
      onCancelled?.()
    } catch (err: unknown) {
      setError(extractApiError(err).message)
      setStatus("idle")
    } finally {
      inFlightRef.current = false
    }
  }

  return (
    <div className="my-2 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm" data-testid="plan-card">
      {/* Header */}
      <div className="mb-2 flex items-center gap-2">
        <span className="text-base" aria-hidden="true">⚠️</span>
        <span className="font-medium text-destructive">需要确认的操作</span>
      </div>

      {/* Description */}
      <p className="mb-2 text-foreground">{plan.preview.description}</p>

      {/* Impact summary */}
      <p className="mb-2 text-muted-foreground text-xs">
        影响 <strong>{plan.preview.affected_count}</strong> 条记录
      </p>

      {/* Sample rows preview */}
      {plan.preview.sample_rows && plan.preview.sample_rows.length > 0 && (
        <div className="mb-3 overflow-x-auto rounded border border-border">
          <table className="w-full text-xs">
            <thead>
              <tr className="bg-muted/50">
                <th className="px-2 py-1 text-left font-medium">商品</th>
                <th className="px-2 py-1 text-left font-medium">变更前</th>
                <th className="px-2 py-1 text-left font-medium">变更后</th>
              </tr>
            </thead>
            <tbody>
              {/* A flagged draft shows every row so no flagged line hides behind "...还有 N 条". */}
              {plan.preview.sample_rows.slice(0, flags.length > 0 ? undefined : 5).map((row, i) => (
                <tr
                  key={i}
                  className={`border-t border-border ${flaggedLines.has(i) ? "bg-destructive/10" : ""}`}
                  data-testid={flaggedLines.has(i) ? "plan-row-flagged" : undefined}
                >
                  <td className="px-2 py-1 text-foreground">{row.name}</td>
                  <td className="px-2 py-1 text-muted-foreground">{row.before}</td>
                  <td className={`px-2 py-1 ${flaggedLines.has(i) ? "font-medium text-destructive" : "text-foreground"}`}>{row.after}</td>
                </tr>
              ))}
              {flags.length === 0 && plan.preview.sample_rows.length > 5 && (
                <tr className="border-t border-border">
                  <td colSpan={3} className="px-2 py-1 text-center text-muted-foreground">
                    ...还有 {plan.preview.sample_rows.length - 5} 条
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Line-by-line confirmation for drafts the rules flagged */}
      {flags.length > 0 && (
        <div className="mb-3 rounded border border-destructive/40 bg-destructive/5 p-2" data-testid="plan-flags">
          <p className="mb-1 text-xs font-medium text-destructive">下面几项可能有误，请逐项核对后打勾：</p>
          <ul className="space-y-1">
            {flags.map((f, i) => (
              <li key={i}>
                <label className="flex items-start gap-2 text-xs text-foreground">
                  <input
                    type="checkbox"
                    checked={!!ticked[i]}
                    onChange={(e) => setTicked((prev) => ({ ...prev, [i]: e.target.checked }))}
                    data-testid="plan-flag-check"
                    className="mt-0.5"
                  />
                  <span>{f.message}</span>
                </label>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Error */}
      {error && (
        <div className="mb-2">
          <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center gap-2">
        <button
          onClick={handleConfirm}
          disabled={status !== "idle" || !allTicked}
          data-testid="plan-confirm-btn"
          className="rounded bg-destructive px-3 py-1.5 text-xs font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50"
        >
          {status === "confirming" ? "执行中..." : "确认执行"}
        </button>
        <button
          onClick={handleCancel}
          disabled={status !== "idle"}
          data-testid="plan-cancel-btn"
          className="rounded border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted disabled:opacity-50"
        >
          {status === "cancelling" ? "取消中..." : "取消"}
        </button>
        <TraceLink traceId={plan.trace_id} />
      </div>
    </div>
  )
}

// TraceLink renders a deep link to the LLM trace in the observability backend
// when both the trace_id and the public host are available. Hidden otherwise.
function TraceLink({ traceId }: { traceId?: string }) {
  const host = process.env.NEXT_PUBLIC_LANGFUSE_HOST
  if (!traceId || !host) return null
  return (
    <a
      href={`${host.replace(/\/$/, "")}/trace/${traceId}`}
      target="_blank"
      rel="noopener noreferrer"
      data-testid="plan-trace-link"
      className="ml-auto text-xs text-muted-foreground underline-offset-2 hover:underline"
    >
      查看推理过程 →
    </a>
  )
}
