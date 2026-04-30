import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { PlanCard } from "./PlanCard"
import type { AIPlan } from "@/lib/api/ai"

// Mock the AI API
vi.mock("@/lib/api/ai", async (importOriginal) => {
  const mod = await importOriginal<typeof import("@/lib/api/ai")>()
  return {
    ...mod,
    confirmPlan: vi.fn().mockResolvedValue(undefined),
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
  })

  it("TestPlanCard_NonPendingPlan_ShowsResolvedState", () => {
    render(<PlanCard plan={makePlan({ status: "confirmed" })} />)

    expect(screen.queryByTestId("plan-confirm-btn")).not.toBeInTheDocument()
    expect(screen.getByText(/已确认/)).toBeInTheDocument()
  })
})
