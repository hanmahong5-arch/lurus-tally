import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { PlanCard } from "./PlanCard"
import type { AIPlan } from "@/lib/api/ai"
import { ApiError } from "@/lib/api/errors"
import { globalUndoStack } from "@/lib/undo/undo-stack"

// cancelPurchaseBill is only called on revert (not exercised here); stub it so the
// real apiFetch is never hit if a test ever does revert.
vi.mock("@/lib/api/purchase", () => ({
  cancelPurchaseBill: vi.fn().mockResolvedValue(undefined),
}))

// Spy on telemetry so we can assert plan_accept_rate fires on confirm/cancel.
vi.mock("@/lib/telemetry", () => ({
  trackEvent: vi.fn(),
}))

// Mock the AI API
vi.mock("@/lib/api/ai", async (importOriginal) => {
  const mod = await importOriginal<typeof import("@/lib/api/ai")>()
  return {
    ...mod,
    confirmPlan: vi.fn().mockResolvedValue({ plan_id: "plan-123", status: "confirmed", type: "price_change", affected_count: 3 }),
    cancelPlan: vi.fn().mockResolvedValue(undefined),
  }
})

const makePlan = (overrides?: Partial<AIPlan>): AIPlan => ({
  id: "plan-123",
  tenant_id: "tenant-abc",
  type: "price_change",
  status: "pending",
  payload: { filter: "all", action: "+5%" },
  preview: {
    description: "Change 3 products by +5%",
    affected_count: 3,
    sample_rows: [
      { name: "Widget A", before: "¥100", after: "+5%" },
      { name: "Widget B", before: "¥200", after: "+5%" },
    ],
  },
  created_at: new Date().toISOString(),
  expires_at: new Date(Date.now() + 30 * 60 * 1000).toISOString(),
  ...overrides,
})

describe("PlanCard", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    globalUndoStack.resetForTest()
  })

  it("TestPlanCard_PendingPlan_ShowsConfirmAndCancelButtons", () => {
    render(<PlanCard plan={makePlan()} />)

    expect(screen.getByTestId("plan-confirm-btn")).toBeInTheDocument()
    expect(screen.getByTestId("plan-cancel-btn")).toBeInTheDocument()
    expect(screen.getByText("Change 3 products by +5%")).toBeInTheDocument()
    expect(screen.getByText(/影响/)).toBeInTheDocument()
  })

  it("TestPlanCard_SampleRows_RendersUpToFiveRows", () => {
    const plan = makePlan({
      preview: {
        description: "Test",
        affected_count: 2,
        sample_rows: [
          { name: "A", before: "1", after: "2" },
          { name: "B", before: "3", after: "4" },
        ],
      },
    })
    render(<PlanCard plan={plan} />)
    expect(screen.getByText("A")).toBeInTheDocument()
    expect(screen.getByText("B")).toBeInTheDocument()
  })

  it("TestPlanCard_CancelButton_CallsCancelPlan", async () => {
    const { cancelPlan } = await import("@/lib/api/ai")
    const onCancelled = vi.fn()

    render(<PlanCard plan={makePlan()} onCancelled={onCancelled} />)

    fireEvent.click(screen.getByTestId("plan-cancel-btn"))

    await waitFor(() => {
      expect(cancelPlan).toHaveBeenCalledWith("plan-123")
    })
    await waitFor(() => {
      expect(onCancelled).toHaveBeenCalled()
    })
  })

  it("TestPlanCard_ConfirmButton_CallsConfirmPlan", async () => {
    const { confirmPlan } = await import("@/lib/api/ai")
    const onConfirmed = vi.fn()

    render(<PlanCard plan={makePlan()} onConfirmed={onConfirmed} />)

    fireEvent.click(screen.getByTestId("plan-confirm-btn"))

    await waitFor(() => {
      expect(confirmPlan).toHaveBeenCalledWith("plan-123")
    })
    await waitFor(() => {
      expect(onConfirmed).toHaveBeenCalled()
    })

    // plan_accept_rate fires with accepted="1" (feeds Kill-switch #2).
    const { trackEvent } = await import("@/lib/telemetry")
    expect(trackEvent).toHaveBeenCalledWith(
      "plan_accept_rate",
      expect.objectContaining({ plan_id: "plan-123", accepted: "1" }),
    )
  })

  it("TestPlanCard_NonPendingPlan_ShowsResolvedState", () => {
    render(<PlanCard plan={makePlan({ status: "confirmed" })} />)

    expect(screen.queryByTestId("plan-confirm-btn")).not.toBeInTheDocument()
    expect(screen.getByText(/已执行/)).toBeInTheDocument()
  })

  it("TestPlanCard_CancelledPlan_ShowsCancelledState", () => {
    render(<PlanCard plan={makePlan({ status: "cancelled" })} />)

    expect(screen.queryByTestId("plan-confirm-btn")).not.toBeInTheDocument()
    expect(screen.getByText(/已取消/)).toBeInTheDocument()
  })

  it("TestPlanCard_ConfirmPurchaseDraft_ShowsBillLink", async () => {
    const { confirmPlan } = await import("@/lib/api/ai")
    vi.mocked(confirmPlan).mockResolvedValueOnce({
      plan_id: "plan-123",
      status: "confirmed",
      type: "create_purchase_draft",
      affected_count: 2,
      bill_id: "bill-789",
      bill_no: "PO-20260522-0001",
    })

    render(<PlanCard plan={makePlan({ type: "create_purchase_draft" })} />)
    fireEvent.click(screen.getByTestId("plan-confirm-btn"))

    const link = await screen.findByTestId("plan-bill-link")
    expect(link).toHaveAttribute("href", "/purchases/bill-789")
    expect(link).toHaveTextContent("PO-20260522-0001")

    // The AI write must be reversible: a matching undo entry is pushed.
    const entry = globalUndoStack.peek()
    expect(entry?.action.type).toBe("ai_purchase_draft")
    // Narrow the discriminated union: `id` only exists on the variants that carry
    // a bill id (ai_stock_adjust / ai_price_change use planId instead).
    if (entry?.action.type === "ai_purchase_draft") {
      expect(entry.action.id).toBe("bill-789")
    }
  })

  it("TestPlanCard_RapidDoubleClick_CallsConfirmOnce", async () => {
    const { confirmPlan } = await import("@/lib/api/ai")
    // Keep confirmPlan in flight so the inFlightRef + disabled-prop guards
    // are both exercised: the second click arrives before React commits the
    // "confirming" state, so disabled is still false — only the synchronous
    // ref latch can collapse it.
    let resolveConfirm: () => void = () => {}
    vi.mocked(confirmPlan).mockImplementation(
      () => new Promise((resolve) => {
        resolveConfirm = () => resolve({ plan_id: "plan-123", status: "confirmed", type: "price_change" })
      })
    )

    render(<PlanCard plan={makePlan()} />)

    const btn = screen.getByTestId("plan-confirm-btn")
    fireEvent.click(btn)
    fireEvent.click(btn)

    await waitFor(() => {
      expect(confirmPlan).toHaveBeenCalledTimes(1)
    })

    resolveConfirm()
  })

  it("TestPlanCard_RapidDoubleClick_CallsCancelOnce", async () => {
    const { cancelPlan } = await import("@/lib/api/ai")
    let resolveCancel: () => void = () => {}
    vi.mocked(cancelPlan).mockImplementation(
      () => new Promise<void>((resolve) => { resolveCancel = resolve })
    )

    render(<PlanCard plan={makePlan()} />)

    const btn = screen.getByTestId("plan-cancel-btn")
    fireEvent.click(btn)
    fireEvent.click(btn)

    await waitFor(() => {
      expect(cancelPlan).toHaveBeenCalledTimes(1)
    })

    resolveCancel()
  })

  it("TestPlanCard_FailedStatus_ShowsFailedCard", () => {
    render(<PlanCard plan={makePlan({ status: "failed" })} />)

    expect(screen.getByTestId("plan-card-failed")).toBeInTheDocument()
    expect(screen.getByText(/执行失败/)).toBeInTheDocument()
    expect(screen.queryByTestId("plan-confirm-btn")).not.toBeInTheDocument()
    // Cancel/关闭 button is present
    expect(screen.getByTestId("plan-cancel-btn")).toBeInTheDocument()
  })

  it("TestPlanCard_FailedStatus_NoConfirmButton", () => {
    render(<PlanCard plan={makePlan({ status: "failed" })} />)

    // confirm button must not appear in failed terminal state
    expect(screen.queryByTestId("plan-confirm-btn")).not.toBeInTheDocument()
  })

  it("TestPlanCard_ConfirmReturns422ExecutionFailed_TransitionsToFailedVisual", async () => {
    const { confirmPlan } = await import("@/lib/api/ai")
    vi.mocked(confirmPlan).mockRejectedValueOnce(
      new ApiError(422, "execution_failed", "库存写入失败，请稍后重试")
    )

    render(<PlanCard plan={makePlan()} />)
    fireEvent.click(screen.getByTestId("plan-confirm-btn"))

    await waitFor(() => {
      expect(screen.getByTestId("plan-card-failed")).toBeInTheDocument()
    })

    expect(screen.getByText(/执行失败/)).toBeInTheDocument()
    expect(screen.getByText(/库存写入失败/)).toBeInTheDocument()
    // Confirm button disappears after transition to failed state
    expect(screen.queryByTestId("plan-confirm-btn")).not.toBeInTheDocument()
  })
})
