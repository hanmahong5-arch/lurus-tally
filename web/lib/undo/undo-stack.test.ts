import { describe, it, expect, beforeEach, vi, afterEach } from "vitest"
import { globalUndoStack, type UndoAction } from "./undo-stack"

function makeAction(id = "id-1", name = "Product A"): UndoAction {
  return { type: "delete_product", id, name, revert: async () => {} }
}

describe("UndoStack", () => {
  beforeEach(() => {
    globalUndoStack.resetForTest()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("TestUndoStack_Push_AddsEntry", () => {
    globalUndoStack.push(makeAction())
    const entry = globalUndoStack.peek()
    expect(entry).toBeDefined()
    expect(entry?.action.type).toBe("delete_product")
  })

  it("TestUndoStack_Pop_ReturnsAndRemovesEntry", () => {
    globalUndoStack.push(makeAction("1", "First"))
    globalUndoStack.push(makeAction("2", "Second"))

    const popped = globalUndoStack.pop()
    expect(popped).toBeDefined()
    expect((popped?.action as { name: string }).name).toBe("Second")
    expect(globalUndoStack.size()).toBe(1)
  })

  it("TestUndoStack_MaxDepth_EvictsOldest", () => {
    for (let i = 0; i < 11; i++) {
      globalUndoStack.push(makeAction(`id-${i}`, `Product ${i}`))
    }
    expect(globalUndoStack.size()).toBe(10)
  })

  it("TestUndoStack_Expiry_StaleEntriesDropped", () => {
    vi.useFakeTimers()

    globalUndoStack.push(makeAction())

    // Advance time past expiry
    vi.advanceTimersByTime(31_000)

    const result = globalUndoStack.pop()
    expect(result).toBeUndefined()
    expect(globalUndoStack.size()).toBe(0)
  })

  it("TestUndoStack_Empty_PopReturnsUndefined", () => {
    const result = globalUndoStack.pop()
    expect(result).toBeUndefined()
  })
})
