import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { renderHook } from "@testing-library/react"
import { fireEvent } from "@testing-library/react"
import { usePosHotkeys } from "./hotkeys"

describe("usePosHotkeys", () => {
  let searchEl: HTMLInputElement
  let qtyEl: HTMLInputElement

  beforeEach(() => {
    searchEl = document.createElement("input")
    qtyEl = document.createElement("input")
    document.body.appendChild(searchEl)
    document.body.appendChild(qtyEl)
  })

  afterEach(() => {
    document.body.removeChild(searchEl)
    document.body.removeChild(qtyEl)
  })

  it("TestUsePosHotkeys_F1_FocusesSearch: pressing F1 calls focus on searchRef", () => {
    const focusSpy = vi.spyOn(searchEl, "focus")
    const searchRef = { current: searchEl }
    const qtyRef = { current: qtyEl }
    const onPayRequested = vi.fn()
    const onCancelRequested = vi.fn()

    renderHook(() =>
      usePosHotkeys({ searchRef, lastQtyRef: qtyRef, onPayRequested, onCancelRequested })
    )

    fireEvent.keyDown(window, { key: "F1" })
    expect(focusSpy).toHaveBeenCalledTimes(1)
  })

  it("TestUsePosHotkeys_F2_FocusesQtyInput: pressing F2 calls focus on lastQtyRef", () => {
    const focusSpy = vi.spyOn(qtyEl, "focus")
    const searchRef = { current: searchEl }
    const qtyRef = { current: qtyEl }
    const onPayRequested = vi.fn()
    const onCancelRequested = vi.fn()

    renderHook(() =>
      usePosHotkeys({ searchRef, lastQtyRef: qtyRef, onPayRequested, onCancelRequested })
    )

    fireEvent.keyDown(window, { key: "F2" })
    expect(focusSpy).toHaveBeenCalledTimes(1)
  })

  it("TestUsePosHotkeys_F3_CallsOnPayRequested: pressing F3 triggers checkout callback", () => {
    const searchRef = { current: searchEl }
    const qtyRef = { current: qtyEl }
    const onPayRequested = vi.fn()
    const onCancelRequested = vi.fn()

    renderHook(() =>
      usePosHotkeys({ searchRef, lastQtyRef: qtyRef, onPayRequested, onCancelRequested })
    )

    fireEvent.keyDown(window, { key: "F3" })
    expect(onPayRequested).toHaveBeenCalledTimes(1)
  })

  it("TestUsePosHotkeys_F4_DispatchesClearConfirm: pressing F4 triggers cancel callback", () => {
    const searchRef = { current: searchEl }
    const qtyRef = { current: qtyEl }
    const onPayRequested = vi.fn()
    const onCancelRequested = vi.fn()

    renderHook(() =>
      usePosHotkeys({ searchRef, lastQtyRef: qtyRef, onPayRequested, onCancelRequested })
    )

    fireEvent.keyDown(window, { key: "F4" })
    expect(onCancelRequested).toHaveBeenCalledTimes(1)
  })

  it("TestUsePosHotkeys_Cleanup_RemovesListener: unmounting removes keydown listener", () => {
    const searchRef = { current: searchEl }
    const qtyRef = { current: qtyEl }
    const onPayRequested = vi.fn()
    const onCancelRequested = vi.fn()

    const { unmount } = renderHook(() =>
      usePosHotkeys({ searchRef, lastQtyRef: qtyRef, onPayRequested, onCancelRequested })
    )

    unmount()
    fireEvent.keyDown(window, { key: "F3" })
    expect(onPayRequested).not.toHaveBeenCalled()
  })
})
