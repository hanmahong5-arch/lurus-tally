import { describe, it, expect, vi, beforeEach } from "vitest"
import { renderHook } from "@testing-library/react"
import { toast } from "sonner"
import { globalUndoStack, type UndoAction } from "@/lib/undo/undo-stack"
import { useUndoShortcut } from "./useUndoShortcut"

// Silence telemetry in tests
vi.mock("@/lib/telemetry", () => ({
  trackEvent: vi.fn(),
}))

// Capture undo-failure toasts.
vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}))

function makeAction(): UndoAction {
  return {
    type: "delete_product",
    id: "prod-1",
    name: "Test Product",
    revert: vi.fn().mockResolvedValue(undefined),
  }
}

function fireCmdZ(target: EventTarget = window) {
  const event = new KeyboardEvent("keydown", {
    key: "z",
    metaKey: true,
    bubbles: true,
  })
  Object.defineProperty(event, "target", { value: target, configurable: true })
  window.dispatchEvent(event)
}

describe("useUndoShortcut", () => {
  beforeEach(() => {
    globalUndoStack.resetForTest()
    vi.mocked(toast.error).mockClear()
  })

  it("TestUseUndoShortcut_CmdZ_CallsOnUndo", () => {
    const action = makeAction()
    globalUndoStack.push(action)

    const onUndo = vi.fn()
    const onEmptyStack = vi.fn()

    renderHook(() => useUndoShortcut({ onUndo, onEmptyStack }))

    fireCmdZ()

    expect(onUndo).toHaveBeenCalledOnce()
    expect(onEmptyStack).not.toHaveBeenCalled()
  })

  it("TestUseUndoShortcut_InsideInput_DoesNotFire", () => {
    const action = makeAction()
    globalUndoStack.push(action)

    const onUndo = vi.fn()
    const onEmptyStack = vi.fn()

    renderHook(() => useUndoShortcut({ onUndo, onEmptyStack }))

    // Create an INPUT element as event target
    const input = document.createElement("input")
    document.body.appendChild(input)

    const event = new KeyboardEvent("keydown", {
      key: "z",
      metaKey: true,
      bubbles: true,
    })
    Object.defineProperty(event, "target", { value: input, configurable: true })
    window.dispatchEvent(event)

    expect(onUndo).not.toHaveBeenCalled()
    expect(onEmptyStack).not.toHaveBeenCalled()

    document.body.removeChild(input)
  })

  it("TestUseUndoShortcut_EmptyStack_CallsOnEmpty", () => {
    const onUndo = vi.fn()
    const onEmptyStack = vi.fn()

    renderHook(() => useUndoShortcut({ onUndo, onEmptyStack }))

    fireCmdZ()

    expect(onEmptyStack).toHaveBeenCalledOnce()
    expect(onUndo).not.toHaveBeenCalled()
  })

  it("TestUseUndoShortcut_RevertFails_ShowsErrorToast", async () => {
    const action: UndoAction = {
      type: "delete_product",
      id: "prod-1",
      name: "Test Product",
      revert: vi.fn().mockRejectedValue(new Error("boom")),
    }
    globalUndoStack.push(action)

    renderHook(() => useUndoShortcut({ onUndo: vi.fn(), onEmptyStack: vi.fn() }))

    fireCmdZ()

    // revert rejects on a later microtask — the catch must surface a toast.
    await vi.waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
  })
})
