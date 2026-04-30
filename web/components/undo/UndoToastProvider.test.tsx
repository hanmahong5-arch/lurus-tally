import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, act } from "@testing-library/react"
import { UndoToastProvider } from "./UndoToastProvider"
import { globalUndoStack, type UndoAction } from "@/lib/undo/undo-stack"

// Silence telemetry calls
vi.mock("@/lib/telemetry", () => ({
  trackEvent: vi.fn(),
}))

function makeAction(name = "商品A"): UndoAction {
  return {
    type: "delete_product",
    id: "prod-1",
    name,
    revert: vi.fn().mockResolvedValue(undefined),
  }
}

function fireCmdZ() {
  const event = new KeyboardEvent("keydown", {
    key: "z",
    metaKey: true,
    bubbles: true,
  })
  window.dispatchEvent(event)
}

describe("UndoToastProvider", () => {
  beforeEach(() => {
    globalUndoStack.resetForTest()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("TestUndoToastProvider_ShowsToastOnCmdZ", async () => {
    globalUndoStack.push(makeAction("商品A"))

    render(
      <UndoToastProvider>
        <div>content</div>
      </UndoToastProvider>
    )

    act(() => {
      fireCmdZ()
    })

    expect(screen.getByRole("status")).toBeInTheDocument()
    expect(screen.getByText(/已撤销/)).toBeInTheDocument()
  })

  it("TestUndoToastProvider_EmptyStack_ShowsDisabledToast", () => {
    render(
      <UndoToastProvider>
        <div>content</div>
      </UndoToastProvider>
    )

    act(() => {
      fireCmdZ()
    })

    expect(screen.getByText("没有可撤销的操作")).toBeInTheDocument()
  })

  it("TestUndoToastProvider_ToastAutoDismisses", () => {
    globalUndoStack.push(makeAction())

    render(
      <UndoToastProvider>
        <div>content</div>
      </UndoToastProvider>
    )

    act(() => {
      fireCmdZ()
    })

    expect(screen.getByRole("status")).toBeInTheDocument()

    // Advance past the 4s dismiss timer
    act(() => {
      vi.advanceTimersByTime(4_001)
    })

    expect(screen.queryByRole("status")).not.toBeInTheDocument()
  })
})
