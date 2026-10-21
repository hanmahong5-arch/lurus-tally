import { describe, it, expect, vi, beforeEach } from "vitest"
import { renderHook, act, waitFor } from "@testing-library/react"

const mem = new Map<string, unknown>()
let storageOk = true
vi.mock("@/lib/draft/idb-storage", () => ({
  draftGet: vi.fn(async (k: string) => mem.get(k)),
  draftSet: vi.fn(async (k: string, v: unknown) => void mem.set(k, v)),
  isDraftStorageAvailable: () => storageOk,
}))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { id: "u1", tenantId: "t1" } }, status: "authenticated" }),
}))
const summary = vi.fn()
vi.mock("@/lib/api/todo", () => ({ getTodoSummary: (...a: unknown[]) => summary(...a) }))

import { useCheckoutQueue } from "./use-checkout-queue"
import { createEntry, queueKey, type QueueEntry } from "@/lib/pos/checkout-queue"
import type { QuickCheckoutRequest } from "@/lib/api/pos"

const body = { items: [], payment_method: "cash", paid_amount: "1.00" } as QuickCheckoutRequest
const KEY = queueKey("t1", "u1")
const ok = () =>
  new Response(JSON.stringify({ bill_id: "i", bill_no: "S1", total_amount: "1", receivable_amount: "0" }), { status: 200 })

describe("useCheckoutQueue", () => {
  beforeEach(() => {
    mem.clear()
    storageOk = true
    summary.mockReset()
    summary.mockResolvedValue({ total: 0 })
    vi.stubGlobal("fetch", vi.fn(async () => ok()))
  })

  it("挂载时回放已有的 pending 并变为 done", async () => {
    mem.set(KEY, [createEntry({ clientRef: "a-1", idempotencyKey: "k", body, now: 1 })])
    const { result } = renderHook(() => useCheckoutQueue())
    await waitFor(() => expect(result.current.entries[0]?.status).toBe("done"))
    expect(result.current.unsent).toBe(0)
  })

  it("online 事件触发回放", async () => {
    const { result } = renderHook(() => useCheckoutQueue())
    await act(async () => {
      await result.current.enqueue({ clientRef: "a-2", idempotencyKey: "k2", body, occurredAt: 1 })
    })
    await act(async () => {
      window.dispatchEvent(new Event("online"))
    })
    await waitFor(() => expect((mem.get(KEY) as QueueEntry[])[0].status).toBe("done"))
  })

  it("本机存储不可用时不入队(避免丢单)", async () => {
    storageOk = false
    const { result } = renderHook(() => useCheckoutQueue())
    let entry: unknown = "x"
    await act(async () => {
      entry = await result.current.enqueue({ clientRef: "a-5", idempotencyKey: "k", body, occurredAt: 1 })
    })
    expect(entry).toBeNull()
  })

  it("能力标志为 false 时不入队", async () => {
    summary.mockResolvedValue({ total: 0, offline_queue_available: false })
    const { result } = renderHook(() => useCheckoutQueue())
    await waitFor(() => expect(result.current.available).toBe(false))
    let entry: unknown = "x"
    await act(async () => {
      entry = await result.current.enqueue({ clientRef: "a-4", idempotencyKey: "k", body, occurredAt: 1 })
    })
    expect(entry).toBeNull()
    expect(mem.get(KEY) ?? []).toEqual([])
  })
})
