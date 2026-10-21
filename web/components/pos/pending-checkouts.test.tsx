import { describe, it, expect, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { PendingCheckouts } from "./pending-checkouts"
import { createEntry, type QueueEntry } from "@/lib/pos/checkout-queue"
import type { QuickCheckoutRequest } from "@/lib/api/pos"

const body = {} as QuickCheckoutRequest
const e = (ref: string, over: Partial<QueueEntry> = {}): QueueEntry => ({
  ...createEntry({ clientRef: ref, idempotencyKey: "k", body, now: 1700000000000, totalAmount: "12.00" }),
  ...over,
})

describe("PendingCheckouts", () => {
  it("没有条目时不渲染", () => {
    const { container } = render(<PendingCheckouts entries={[]} onRetry={vi.fn()} onDiscard={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })

  it("角标显示未上传数，点开列出单号金额状态", () => {
    render(
      <PendingCheckouts
        entries={[e("abcd1234-7"), e("abcd1234-8", { status: "done", server_bill_no: "S8" })]}
        onRetry={vi.fn()}
        onDiscard={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByText("待上传 1"))
    expect(screen.getByText("OFF-abcd-7")).toBeInTheDocument()
    expect(screen.getByText("正式单号 S8")).toBeInTheDocument()
  })

  it("failed 可重试，放弃经确认后才触发", async () => {
    const onRetry = vi.fn()
    const onDiscard = vi.fn()
    render(
      <PendingCheckouts
        entries={[e("abcd1234-9", { status: "failed", last_error: "库存不足" })]}
        onRetry={onRetry}
        onDiscard={onDiscard}
      />,
    )
    fireEvent.click(screen.getByText("待上传 1"))
    expect(screen.getByText("库存不足")).toBeInTheDocument()
    fireEvent.click(screen.getByText("重试"))
    expect(onRetry).toHaveBeenCalledWith("abcd1234-9")
    fireEvent.click(screen.getByText("放弃"))
    await vi.waitFor(() => expect(onDiscard).toHaveBeenCalledWith("abcd1234-9"))
  })
})
