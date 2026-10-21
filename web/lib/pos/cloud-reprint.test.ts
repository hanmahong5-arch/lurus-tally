import { describe, it, expect, vi, beforeEach } from "vitest"

vi.mock("@/lib/draft/idb-storage", () => {
  const store = new Map<string, unknown>()
  return {
    draftGet: vi.fn(async (k: string) => store.get(k)),
    draftSet: vi.fn(async (k: string, v: unknown) => void store.set(k, v)),
    _store: store,
  }
})
vi.mock("@/hooks/use-cloud-print", () => ({ readCloudPrintInfo: () => null }))
vi.mock("@/lib/pos/cloud-print-send", () => ({ sendCloudReceipt: vi.fn() }))

import * as idb from "@/lib/draft/idb-storage"
import { createEntry, loadQueue, queueKey, replayDue, type QueueEntry } from "./checkout-queue"
import type { QuickCheckoutRequest } from "@/lib/api/pos"
import { reprintAfterReplay } from "./cloud-reprint"

const mem = (idb as unknown as { _store: Map<string, unknown> })._store
const KEY = queueKey("t1", "u1")
const body = { items: [], payment_method: "cash", paid_amount: "1.00" } as QuickCheckoutRequest
const on = () => ({ available: true, configured: true, autoPrint: true })

function pending(ref: string): QueueEntry {
  return createEntry({ clientRef: ref, idempotencyKey: `k-${ref}`, body, now: 1000 })
}

beforeEach(() => mem.clear())

describe("联网回放后补发云打印", () => {
  it("done 后只补发一次，重复触发不再发", async () => {
    mem.set(KEY, [pending("a")])
    const send = vi.fn().mockResolvedValue(true)
    const sendEntry = vi.fn().mockResolvedValue({ kind: "ok", billNo: "S1", billId: "bill-1" })
    const onDone = (e: QueueEntry) => reprintAfterReplay(KEY, e, { info: on, send }).then(() => undefined)
    const out = await replayDue(KEY, { now: () => 2000, send: sendEntry, onDone })
    expect(send).toHaveBeenCalledTimes(1)
    expect(send).toHaveBeenCalledWith("bill-1", expect.objectContaining({ force: true }))
    expect(out[0].cloud_printed).toBe(true)
    // 再来一次（例如下一轮回放又拿到同一条 done）：不再发
    const done = (await loadQueue(KEY))[0]
    expect(await reprintAfterReplay(KEY, done, { info: on, send })).toBe(false)
    await reprintAfterReplay(KEY, { ...done, cloud_printed: false }, { info: on, send })
    expect(send).toHaveBeenCalledTimes(1)
  })

  it("没开自动打印或没有服务器单据 id 时不发", async () => {
    const send = vi.fn()
    const done: QueueEntry = { ...pending("a"), status: "done", server_bill_id: "b1" }
    mem.set(KEY, [done])
    expect(await reprintAfterReplay(KEY, done, { info: () => ({ available: true, configured: true, autoPrint: false }), send })).toBe(false)
    expect(await reprintAfterReplay(KEY, { ...done, server_bill_id: undefined }, { info: on, send })).toBe(false)
    expect(send).not.toHaveBeenCalled()
  })
})
