import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"

vi.mock("@/lib/draft/idb-storage", () => {
  const store = new Map<string, unknown>()
  return {
    draftGet: vi.fn(async (k: string) => store.get(k)),
    draftSet: vi.fn(async (k: string, v: unknown) => void store.set(k, v)),
    _store: store,
  }
})

import * as idb from "@/lib/draft/idb-storage"
import { ApiError, NetworkError } from "@/lib/api/errors"
import type { QuickCheckoutRequest } from "@/lib/api/pos"
import {
  BACKOFF_MAX_MS,
  DONE_KEEP_MS,
  applyOutcome,
  backoffMs,
  createEntry,
  discardEntry,
  loadQueue,
  newClientRef,
  nextDue,
  offlineRefOf,
  pruneDone,
  queueKey,
  replayDue,
  retryEntry,
  sendEntry,
  shouldEnqueue,
  type QueueEntry,
} from "./checkout-queue"

const mem = (idb as unknown as { _store: Map<string, unknown> })._store
const body = { items: [], payment_method: "cash", paid_amount: "1.00" } as QuickCheckoutRequest
const KEY = queueKey("t1", "u1")

function entry(ref: string, over: Partial<QueueEntry> = {}): QueueEntry {
  return { ...createEntry({ clientRef: ref, idempotencyKey: `k-${ref}`, body, now: 1000 }), ...over }
}

function jsonRes(status: number, payload: unknown): Response {
  return new Response(JSON.stringify(payload), { status, headers: { "content-type": "application/json" } })
}

const billOk = { bill_id: "i", bill_no: "S1", total_amount: "1", receivable_amount: "0" }

describe("入队判定", () => {
  it("只有请求从未发出的网络错才入队", () => {
    expect(shouldEnqueue(new NetworkError("offline", "offline", false))).toBe(true)
    expect(shouldEnqueue(new NetworkError("network", "x", false))).toBe(true)
  })
  it("请求可能已送达的网络错与任何服务器错误都不入队", () => {
    expect(shouldEnqueue(new NetworkError("network", "cut", true))).toBe(false)
    expect(shouldEnqueue(new ApiError(500, "e", "m"))).toBe(false)
    expect(shouldEnqueue(new ApiError(422, "e", "m"))).toBe(false)
    expect(shouldEnqueue(new ApiError(409, "e", "m"))).toBe(false)
    expect(shouldEnqueue(new Error("x"))).toBe(false)
  })
})

describe("单号", () => {
  beforeEach(() => window.localStorage.clear())
  it("client_ref 形如 设备id前8位-序号，序号单调递增，离线单号取前4位", () => {
    const a = newClientRef()
    const b = newClientRef()
    expect(a).toMatch(/^[0-9a-f]{8}-1$/)
    expect(b.endsWith("-2")).toBe(true)
    expect(a.split("-")[0]).toBe(b.split("-")[0])
    expect(offlineRefOf(a)).toBe(`OFF-${a.slice(0, 4)}-1`)
  })
})

describe("状态机（纯函数）", () => {
  it("成功与已存在都进 done 并记单号", () => {
    const ok = applyOutcome([entry("a")], "a", { kind: "ok", billNo: "S1" }, 5000)[0]
    expect(ok).toMatchObject({ status: "done", server_bill_no: "S1", done_at: 5000 })
    const ex = applyOutcome([entry("a")], "a", { kind: "exists", billNo: "S2" }, 5000)[0]
    expect(ex).toMatchObject({ status: "done", server_bill_no: "S2" })
  })
  it("拒绝进 failed；重试回到 pending 清零；放弃只删 failed", () => {
    const f = applyOutcome([entry("a")], "a", { kind: "rejected", message: "库存不足" }, 1)
    expect(f[0]).toMatchObject({ status: "failed", last_error: "库存不足" })
    expect(discardEntry([entry("b")], "b")).toHaveLength(1)
    expect(retryEntry(f, "a")[0]).toMatchObject({ status: "pending", attempts: 0 })
    expect(discardEntry(f, "a")).toHaveLength(0)
  })
  it("retry 保持 pending、attempts+1、指数退避且封顶 5 分钟", () => {
    let es = [entry("a")]
    es = applyOutcome(es, "a", { kind: "retry", message: "x" }, 0)
    expect(es[0]).toMatchObject({ status: "pending", attempts: 1, next_attempt_at: backoffMs(1) })
    expect(nextDue(es, backoffMs(1) - 1)).toBeUndefined()
    expect(nextDue(es, backoffMs(1))?.client_ref).toBe("a")
    expect(backoffMs(2)).toBe(2 * backoffMs(1))
    expect(backoffMs(30)).toBe(BACKOFF_MAX_MS)
  })
  it("done 满 24 小时后清理", () => {
    const es = [entry("a", { status: "done", done_at: 0 }), entry("b", { status: "done", done_at: DONE_KEEP_MS })]
    expect(pruneDone(es, DONE_KEEP_MS + 1).map((e) => e.client_ref)).toEqual(["b"])
  })
})

describe("回放（mock fetch）", () => {
  beforeEach(() => {
    mem.clear()
    vi.stubGlobal("fetch", vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  const fetchMock = () => vi.mocked(globalThis.fetch)

  it("成功：带原 Idempotency-Key 与 client_ref，进 done", async () => {
    mem.set(KEY, [entry("a", { body: { ...body, client_ref: "a" } })])
    fetchMock().mockResolvedValue(jsonRes(200, billOk))
    const out = await replayDue(KEY, { now: () => 2000 })
    expect(out[0]).toMatchObject({ status: "done", server_bill_no: "S1" })
    const [, init] = fetchMock().mock.calls[0]
    expect((init as RequestInit).headers).toMatchObject({ "Idempotency-Key": "k-a" })
    expect(JSON.parse((init as RequestInit).body as string).client_ref).toBe("a")
  })

  it("409 checkout_already_exists 视为 done 并记已有单号", async () => {
    mem.set(KEY, [entry("a")])
    fetchMock().mockResolvedValue(jsonRes(409, { code: "checkout_already_exists", id: "i", bill_no: "S3" }))
    const out = await replayDue(KEY, { now: () => 2000 })
    expect(out[0]).toMatchObject({ status: "done", server_bill_no: "S3" })
  })

  it("422 进 failed，且不再自动重放", async () => {
    mem.set(KEY, [entry("a")])
    fetchMock().mockResolvedValue(jsonRes(422, { message: "库存不足" }))
    const out = await replayDue(KEY, { now: () => 2000 })
    expect(out[0]).toMatchObject({ status: "failed", last_error: "库存不足" })
    await replayDue(KEY, { now: () => 999999 })
    expect(fetchMock()).toHaveBeenCalledTimes(1)
  })

  it("网络错保持 pending 并停止本轮（不连发后面的单）", async () => {
    mem.set(KEY, [entry("a"), entry("b")])
    fetchMock().mockRejectedValue(new TypeError("Failed to fetch"))
    const out = await replayDue(KEY, { now: () => 2000 })
    expect(out.map((e) => e.status)).toEqual(["pending", "pending"])
    expect(out[0].attempts).toBe(1)
    expect(fetchMock()).toHaveBeenCalledTimes(1)
  })

  it("5xx 保持 pending 并退避", async () => {
    mem.set(KEY, [entry("a")])
    fetchMock().mockResolvedValue(jsonRes(503, { error: "x" }))
    const out = await replayDue(KEY, { now: () => 2000 })
    expect(out[0]).toMatchObject({ status: "pending", attempts: 1, next_attempt_at: 2000 + backoffMs(1) })
  })

  it("一次一条串行：成功后继续下一条", async () => {
    mem.set(KEY, [entry("a"), entry("b")])
    fetchMock().mockImplementation(async () => jsonRes(200, billOk))
    const out = await replayDue(KEY, { now: () => 2000 })
    expect(out.map((e) => e.status)).toEqual(["done", "done"])
    expect(fetchMock()).toHaveBeenCalledTimes(2)
    expect((await loadQueue(KEY)).every((e) => e.status === "done")).toBe(true)
  })

  it("残留的 sending 重新回放", async () => {
    mem.set(KEY, [entry("a", { status: "sending" })])
    fetchMock().mockResolvedValue(jsonRes(200, billOk))
    const out = await replayDue(KEY, { now: () => 2000 })
    expect(out[0].status).toBe("done")
  })

  it("401 保持待传而不是失败", async () => {
    fetchMock().mockResolvedValue(jsonRes(401, { error: "unauthorized" }))
    expect((await sendEntry(entry("a"))).kind).toBe("retry")
  })
})
