"use client"

import { useState } from "react"
import { type AIPlan, confirmPlan, cancelPlan } from "@/lib/api/ai"

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
  const [status, setStatus] = useState<"idle" | "confirming" | "cancelling" | "done">("idle")
  const [error, setError] = useState<string | null>(null)

  if (plan.status !== "pending" || status === "done") {
    return (
      <div className="my-2 rounded-lg border border-border bg-muted/50 p-3 text-sm text-muted-foreground">
        {plan.status === "confirmed" || status === "done"
          ? "✓ 操作已确认执行"
          : "✗ 操作已取消"}
      </div>
    )
  }

  const handleConfirm = async () => {
    setStatus("confirming")
    setError(null)
    try {
      await confirmPlan(plan.id)
      setStatus("done")
      onConfirmed?.()
    } catch (err: unknown) {
      setError(String(err))
      setStatus("idle")
    }
  }

  const handleCancel = async () => {
    setStatus("cancelling")
    setError(null)
    try {
      await cancelPlan(plan.id)
      setStatus("done")
      onCancelled?.()
    } catch (err: unknown) {
      setError(String(err))
      setStatus("idle")
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
        <div className="mb-3 overflow-hidden rounded border border-border">
          <table className="w-full text-xs">
            <thead>
              <tr className="bg-muted/50">
                <th className="px-2 py-1 text-left font-medium">商品</th>
                <th className="px-2 py-1 text-left font-medium">变更前</th>
                <th className="px-2 py-1 text-left font-medium">变更后</th>
              </tr>
            </thead>
            <tbody>
              {plan.preview.sample_rows.slice(0, 5).map((row, i) => (
                <tr key={i} className="border-t border-border">
                  <td className="px-2 py-1 text-foreground">{row.name}</td>
                  <td className="px-2 py-1 text-muted-foreground">{row.before}</td>
                  <td className="px-2 py-1 text-foreground">{row.after}</td>
                </tr>
              ))}
              {plan.preview.sample_rows.length > 5 && (
                <tr className="border-t border-border">
                  <td colSpan={3} className="px-2 py-1 text-center text-muted-foreground">
                    …还有 {plan.preview.sample_rows.length - 5} 条
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Error */}
      {error && (
        <p className="mb-2 text-xs text-destructive">{error}</p>
      )}

      {/* Actions */}
      <div className="flex gap-2">
        <button
          onClick={handleConfirm}
          disabled={status !== "idle"}
          data-testid="plan-confirm-btn"
          className="rounded bg-destructive px-3 py-1.5 text-xs font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50"
        >
          {status === "confirming" ? "执行中…" : "确认执行"}
        </button>
        <button
          onClick={handleCancel}
          disabled={status !== "idle"}
          data-testid="plan-cancel-btn"
          className="rounded border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted disabled:opacity-50"
        >
          {status === "cancelling" ? "取消中…" : "取消"}
        </button>
      </div>
    </div>
  )
}
