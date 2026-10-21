/**
 * 结账本地台账：断网时「请求从未发出」的快速收银单先存进 IndexedDB，联网后按原
 * Idempotency-Key 与 client_ref 逐条回放。服务器永远是真源，这里不做冲突合并：
 *   成功 / 已存在(409 checkout_already_exists) -> done
 *   其它 4xx（人工处理）                        -> failed
 *   网络错 / 5xx / 登录过期 / 409 处理中        -> 保持 pending，指数退避，上限 5 分钟
 * 状态机是纯函数，持久化与网络调用分开，便于测试。
 */
import { draftGet, draftSet } from "@/lib/draft/idb-storage"
import { apiFetch } from "@/lib/api/client"
import { ApiError, NetworkError } from "@/lib/api/errors"
import type { QuickCheckoutRequest, QuickCheckoutResult } from "@/lib/api/pos"
import { CHECKOUT_SEQ_KEY, DEVICE_ID_KEY } from "@/lib/device-keys"

export type QueueStatus = "pending" | "sending" | "failed" | "done"

export interface QueueEntry {
  client_ref: string
  idempotency_key: string
  body: QuickCheckoutRequest
  status: QueueStatus
  created_at: number
  /** 销售真正发生的时间（毫秒时间戳）。 */
  occurred_at: number
  attempts: number
  last_error?: string
  server_bill_no?: string
  /** 服务器单据 id；补发云打印用。409 已存在时服务器不一定回，可能为空。 */
  server_bill_id?: string
  /** 下次允许回放的时间（毫秒）；pending 才有意义。 */
  next_attempt_at?: number
  /** 进入 done 的时间，用于 24 小时后清理。 */
  done_at?: number
  /** 展示用：总金额。 */
  total_amount?: string
  /** 入队时记下的商品名（product_id -> 名称），离线小票用；请求体里只有 id。 */
  item_names?: Record<string, string>
  /** 联网回放成功后是否已补发过云打印（每条只补发一次）。 */
  cloud_printed?: boolean
}

export type SendOutcome =
  | { kind: "ok"; billNo: string; billId?: string }
  | { kind: "exists"; billNo?: string; billId?: string }
  | { kind: "rejected"; message: string }
  | { kind: "retry"; message: string }

export const BACKOFF_BASE_MS = 5_000
export const BACKOFF_MAX_MS = 5 * 60_000
export const DONE_KEEP_MS = 24 * 3600_000

export function queueKey(tenantId: string, userId: string): string {
  return `pos.checkout.queue@${tenantId}:${userId}`
}

// ── 设备 id 与单号 ────────────────────────────────────────────────────────────

function randomId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID()
  return Date.now().toString(16) + Math.random().toString(16).slice(2, 14)
}

/** 本设备的随机 id（localStorage 持久化，退出登录不清）。 */
export function getDeviceId(): string {
  try {
    const cur = window.localStorage.getItem(DEVICE_ID_KEY)
    if (cur) return cur
    const id = randomId()
    window.localStorage.setItem(DEVICE_ID_KEY, id)
    return id
  } catch {
    return randomId()
  }
}

/** 取下一个本地序号（单调递增，持久化）。 */
export function nextSeq(): number {
  try {
    const n = Number(window.localStorage.getItem(CHECKOUT_SEQ_KEY))
    const next = (Number.isFinite(n) && n > 0 ? Math.floor(n) : 0) + 1
    window.localStorage.setItem(CHECKOUT_SEQ_KEY, String(next))
    return next
  } catch {
    return Date.now()
  }
}

/** client_ref = <设备id前8位>-<本地序号>，不超过 64 字符。 */
export function newClientRef(): string {
  return `${getDeviceId().replace(/-/g, "").slice(0, 8)}-${nextSeq()}`
}

/** 离线单号展示：OFF-<设备id前4位>-<序号>。 */
export function offlineRefOf(clientRef: string): string {
  const i = clientRef.indexOf("-")
  if (i < 0) return `OFF-${clientRef}`
  return `OFF-${clientRef.slice(0, Math.min(4, i))}-${clientRef.slice(i + 1)}`
}

// ── 入队判定 ──────────────────────────────────────────────────────────────────

/**
 * 只有「请求从未发出」才允许入队：offline 预检、或连接在任何回复前就失败。
 * 服务器返回的任何 4xx/5xx、请求发出后丢了回复（sent=true）都不入队。
 */
export function shouldEnqueue(err: unknown): boolean {
  return err instanceof NetworkError && err.sent === false
}

// ── 纯函数状态机 ──────────────────────────────────────────────────────────────

export function backoffMs(attempts: number): number {
  const n = Math.max(1, attempts)
  return Math.min(BACKOFF_MAX_MS, BACKOFF_BASE_MS * 2 ** (n - 1))
}

export function createEntry(args: {
  clientRef: string
  idempotencyKey: string
  body: QuickCheckoutRequest
  now: number
  occurredAt?: number
  totalAmount?: string
  itemNames?: Record<string, string>
}): QueueEntry {
  return {
    client_ref: args.clientRef,
    idempotency_key: args.idempotencyKey,
    body: args.body,
    status: "pending",
    created_at: args.now,
    occurred_at: args.occurredAt ?? args.now,
    attempts: 0,
    total_amount: args.totalAmount,
    item_names: args.itemNames,
    cloud_printed: false,
  }
}

/** 标记已补发云打印；先标记再发，宁可少打一张，也不重复出纸。 */
export function markCloudPrinted(entries: QueueEntry[], ref: string): QueueEntry[] {
  return patch(entries, ref, (e) => ({ ...e, cloud_printed: true }))
}

/** 加载后把残留的 sending（上次页面中途关闭）还原成 pending。 */
export function recoverSending(entries: QueueEntry[]): QueueEntry[] {
  return entries.map((e) => (e.status === "sending" ? { ...e, status: "pending" as const } : e))
}

/** 下一条到期的 pending；一次只回放一条。 */
export function nextDue(entries: QueueEntry[], now: number): QueueEntry | undefined {
  return entries.find((e) => e.status === "pending" && (e.next_attempt_at ?? 0) <= now)
}

function patch(entries: QueueEntry[], ref: string, fn: (e: QueueEntry) => QueueEntry): QueueEntry[] {
  return entries.map((e) => (e.client_ref === ref ? fn(e) : e))
}

export function markSending(entries: QueueEntry[], ref: string): QueueEntry[] {
  return patch(entries, ref, (e) => (e.status === "pending" ? { ...e, status: "sending" } : e))
}

export function applyOutcome(entries: QueueEntry[], ref: string, out: SendOutcome, now: number): QueueEntry[] {
  return patch(entries, ref, (e) => {
    switch (out.kind) {
      case "ok":
        return { ...e, status: "done", server_bill_no: out.billNo, server_bill_id: out.billId ?? e.server_bill_id, done_at: now, last_error: undefined, next_attempt_at: undefined }
      case "exists":
        return { ...e, status: "done", server_bill_no: out.billNo ?? e.server_bill_no, server_bill_id: out.billId ?? e.server_bill_id, done_at: now, last_error: undefined, next_attempt_at: undefined }
      case "rejected":
        return { ...e, status: "failed", attempts: e.attempts + 1, last_error: out.message, next_attempt_at: undefined }
      case "retry": {
        const attempts = e.attempts + 1
        return { ...e, status: "pending", attempts, last_error: out.message, next_attempt_at: now + backoffMs(attempts) }
      }
    }
  })
}

/** 人工重试：failed -> pending，立即可回放。 */
export function retryEntry(entries: QueueEntry[], ref: string): QueueEntry[] {
  return patch(entries, ref, (e) =>
    e.status === "failed" ? { ...e, status: "pending", attempts: 0, last_error: undefined, next_attempt_at: undefined } : e,
  )
}

/** 放弃：只允许放弃 failed 的条目。 */
export function discardEntry(entries: QueueEntry[], ref: string): QueueEntry[] {
  return entries.filter((e) => !(e.client_ref === ref && e.status === "failed"))
}

/** done 保留 24 小时后清理。 */
export function pruneDone(entries: QueueEntry[], now: number): QueueEntry[] {
  return entries.filter((e) => !(e.status === "done" && now - (e.done_at ?? e.created_at) > DONE_KEEP_MS))
}

/** 还没落到服务器的条目数（pending / sending / failed）。 */
export function unsentCount(entries: QueueEntry[]): number {
  return entries.filter((e) => e.status !== "done").length
}

// ── 回放一条（网络） ──────────────────────────────────────────────────────────

/** 把一次回放抛出的错误归类。 */
export function classifyError(err: unknown): SendOutcome {
  if (err instanceof NetworkError) return { kind: "retry", message: "网络不通，等待重试" }
  if (err instanceof ApiError) {
    const b = (err.body ?? {}) as { code?: unknown; error?: unknown; bill_no?: unknown; bill_id?: unknown; id?: unknown; message?: unknown }
    if (err.status === 409) {
      if (b.code === "checkout_already_exists" || b.error === "checkout_already_exists") {
        return {
          kind: "exists",
          billNo: typeof b.bill_no === "string" ? b.bill_no : undefined,
          billId: typeof b.id === "string" ? b.id : typeof b.bill_id === "string" ? b.bill_id : undefined,
        }
      }
      // 例如 idempotency_in_flight：上一次还在处理，稍后再试
      return { kind: "retry", message: "服务器还在处理上一次请求" }
    }
    // 登录过期 / 请求超时 / 限流：不是单据的问题，保持 pending
    if (err.status === 401 || err.status === 408 || err.status === 429) {
      return { kind: "retry", message: err.status === 401 ? "登录已过期，重新登录后继续上传" : "服务器繁忙，稍后重试" }
    }
    if (err.status >= 500) return { kind: "retry", message: "服务暂时不可用" }
    const msg = typeof b.message === "string" && b.message ? b.message : err.message
    return { kind: "rejected", message: msg || `服务器拒绝了这笔单（${err.status}）` }
  }
  return { kind: "retry", message: err instanceof Error ? err.message : "上传失败" }
}

/** 用原 Idempotency-Key 与 client_ref 回放一条。不弹提示，由台账角标展示结果。 */
export async function sendEntry(entry: QueueEntry, tenantId?: string): Promise<SendOutcome> {
  try {
    const res = await apiFetch<QuickCheckoutResult>("/sale-bills/quick-checkout", {
      method: "POST",
      body: JSON.stringify(entry.body),
      tenantId,
      idempotencyKey: entry.idempotency_key,
      silent: true,
    })
    return { kind: "ok", billNo: res.bill_no, billId: res.bill_id }
  } catch (err) {
    return classifyError(err)
  }
}

// ── 持久化（与状态机分离） ────────────────────────────────────────────────────

export async function loadQueue(key: string): Promise<QueueEntry[]> {
  const raw = await draftGet<QueueEntry[]>(key)
  return Array.isArray(raw) ? raw : []
}

export async function saveQueue(key: string, entries: QueueEntry[]): Promise<void> {
  await draftSet(key, entries)
}

/** 未落地的条目数（退出登录前确认用）。 */
export async function countUnsent(key: string): Promise<number> {
  return unsentCount(await loadQueue(key))
}

const chains = new Map<string, Promise<unknown>>()

/** 串行读-改-写：同一队列的所有修改排队执行，避免回放与新入队互相覆盖。 */
export function mutateQueue(key: string, fn: (entries: QueueEntry[]) => QueueEntry[]): Promise<QueueEntry[]> {
  const prev = chains.get(key) ?? Promise.resolve()
  const run = prev.then(async () => {
    const next = fn(await loadQueue(key))
    await saveQueue(key, next)
    return next
  })
  chains.set(
    key,
    run.catch(() => undefined),
  )
  return run
}

/**
 * 回放全部到期条目（一次一条，串行）。每步先持久化 sending 再发请求，
 * 结果写回后继续下一条；遇到网络错就停止本轮，避免对断网的链路连发。
 */
export async function replayDue(
  key: string,
  opts: { tenantId?: string; now?: () => number; send?: typeof sendEntry; onChange?: (e: QueueEntry[]) => void; onDone?: (e: QueueEntry) => void | Promise<void> } = {},
): Promise<QueueEntry[]> {
  const now = opts.now ?? Date.now
  const send = opts.send ?? sendEntry
  let latest = await mutateQueue(key, (es) => pruneDone(recoverSending(es), now()))
  for (let guard = 0; guard < 200; guard++) {
    const due = nextDue(latest, now())
    if (!due) break
    latest = await mutateQueue(key, (es) => markSending(es, due.client_ref))
    opts.onChange?.(latest)
    const out = await send(due, opts.tenantId)
    latest = await mutateQueue(key, (es) => applyOutcome(es, due.client_ref, out, now()))
    opts.onChange?.(latest)
    if (out.kind === "ok" || out.kind === "exists") {
      const doneEntry = latest.find((e) => e.client_ref === due.client_ref)
      // 回调出错（例如云打印失败）不能影响后面的单继续回放。
      if (doneEntry) await Promise.resolve(opts.onDone?.(doneEntry)).catch(() => undefined)
      latest = await loadQueue(key)
      opts.onChange?.(latest)
    }
    if (out.kind === "retry") break
  }
  return latest
}
