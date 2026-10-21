import { describe, it, expect, vi, afterEach } from "vitest"
import { render, act } from "@testing-library/react"

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))

import { PrintShell, PRINT_DONE_MESSAGE, PRINT_INCOMPLETE_MESSAGE } from "./print-shell"

const realParent = Object.getOwnPropertyDescriptor(window, "parent")

function embedAsFrame() {
  const post = vi.fn()
  Object.defineProperty(window, "parent", { configurable: true, value: { postMessage: post } })
  return post
}

function shell(notice?: string) {
  return (
    <PrintShell title="销售小票" paper="80" options={["80"]} onPaper={() => {}} autoPrint ready notice={notice}>
      <div>小票</div>
    </PrintShell>
  )
}

describe("PrintShell hidden frame", () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    if (realParent) Object.defineProperty(window, "parent", realParent)
  })

  it("prints and reports done when the document is complete", () => {
    vi.useFakeTimers()
    const post = embedAsFrame()
    const print = vi.spyOn(window, "print").mockImplementation(() => {})
    render(shell())
    act(() => {
      vi.advanceTimersByTime(400)
    })
    expect(print).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(PRINT_DONE_MESSAGE, window.location.origin)
  })

  it("does not print an incomplete receipt, tells the till instead", () => {
    vi.useFakeTimers()
    const post = embedAsFrame()
    const print = vi.spyOn(window, "print").mockImplementation(() => {})
    render(shell("有商品信息没加载出来，请刷新后再打印"))
    act(() => {
      vi.advanceTimersByTime(400)
    })
    expect(print).not.toHaveBeenCalled()
    expect(post).toHaveBeenCalledWith(PRINT_INCOMPLETE_MESSAGE, window.location.origin)
  })
})

describe("PrintShell sign-out broadcast", () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.localStorage.clear()
  })

  it("goes to the switch login when another tab signs out", () => {
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      render(shell())
      window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: "123" }))
      expect(assign).toHaveBeenCalledWith("/login?switch=1")
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })
})
