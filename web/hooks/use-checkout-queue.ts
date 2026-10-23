"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { useSession } from "next-auth/react"
import { getMeBrowser } from "@/lib/api/me"
import { isDraftStorageAvailable } from "@/lib/draft/idb-storage"
import type { QuickCheckoutRequest } from "@/lib/api/pos"
import {
  createEntry,
  discardEntry,
  loadQueue,
  mutateQueue,
  pruneDone,
  queueKey,
  recoverSending,
  replayDue,
  retryEntry,
  unsentCount,
  type QueueEntry,
} from "@/lib/pos/checkout-queue"
import { reprintAfterReplay } from "@/lib/pos/cloud-reprint"

/** 定时回放间隔。 */
export const REPLAY_INTERVAL_MS = 30_000

/** 当前登录人的队列键；会话没解析出租户或用户时为 null。 */
export function useQueueKey(): { key: string | null; tenantId?: string } {
  const session = useSession()
  const user = session?.data?.user
  const tenantId = user?.tenantId ?? undefined
  const userId = user?.id
  return { key: tenantId && userId ? queueKey(tenantId, userId) : null, tenantId }
}

/**
 * 结账本地台账的 React 外壳：挂载、online 事件、每 30s 串行回放 pending；
 * 能力标志 offline_queue_available 为 false 时不启用（入队方应改为报错）。
 */
export function useCheckoutQueue() {
  const { key, tenantId } = useQueueKey()
  const [entries, setEntries] = useState<QueueEntry[]>([])
  // Fail-open until /me answers: offline POS still needs to enqueue when the
  // capability probe itself is unreachable. Backend false disables the queue.
  const [available, setAvailable] = useState(true)
  const runningRef = useRef(false)

  useEffect(() => {
    const ctrl = new AbortController()
    getMeBrowser({ tenantId, signal: ctrl.signal })
      .then((me) => {
        if (ctrl.signal.aborted) return
        setAvailable(me.offline_queue_available ?? true)
      })
      .catch(() => {})
    return () => ctrl.abort()
  }, [tenantId])

  const replay = useCallback(async () => {
    if (!key || !available || runningRef.current) return
    if (typeof navigator !== "undefined" && navigator.onLine === false) return
    runningRef.current = true
    try {
      await replayDue(key, {
        tenantId,
        onChange: setEntries,
        // 离线单补发云打印：只在上传成功后、且店铺开了自动打印时发一次。
        onDone: async (e) => {
          await reprintAfterReplay(key, e, { tenantId })
        },
      })
    } finally {
      runningRef.current = false
    }
  }, [key, tenantId, available])

  // 挂载先读出已有条目（离线时也要显示角标），再回放一轮。
  useEffect(() => {
    if (!key) {
      setEntries([])
      return
    }
    let cancelled = false
    loadQueue(key).then((es) => {
      if (!cancelled) setEntries(pruneDone(recoverSending(es), Date.now()))
    })
    void replay()
    return () => {
      cancelled = true
    }
  }, [key, replay])

  useEffect(() => {
    if (!key) return
    const onOnline = () => void replay()
    window.addEventListener("online", onOnline)
    const timer = setInterval(() => void replay(), REPLAY_INTERVAL_MS)
    return () => {
      window.removeEventListener("online", onOnline)
      clearInterval(timer)
    }
  }, [key, replay])

  /** 入队；队列不可用时返回 null，调用方应照常报错。 */
  const enqueue = useCallback(
    async (args: { clientRef: string; idempotencyKey: string; body: QuickCheckoutRequest; occurredAt: number; totalAmount?: string; itemNames?: Record<string, string> }) => {
      if (!key || !available) return null
      // 本机存储不可用（隐私模式、配额满）时落不了盘，入队会丢单：照常报错、保留购物车。
      if (!isDraftStorageAvailable()) return null
      const entry = createEntry({
        clientRef: args.clientRef,
        idempotencyKey: args.idempotencyKey,
        body: args.body,
        now: Date.now(),
        occurredAt: args.occurredAt,
        totalAmount: args.totalAmount,
        itemNames: args.itemNames,
      })
      const next = await mutateQueue(key, (es) => [...es, entry])
      // 写入过程中才触发存储错误时,标志会翻转：此时并未真正落盘。
      if (!isDraftStorageAvailable()) return null
      setEntries(next)
      return entry
    },
    [key, available],
  )

  const retry = useCallback(
    async (ref: string) => {
      if (!key) return
      setEntries(await mutateQueue(key, (es) => retryEntry(es, ref)))
      void replay()
    },
    [key, replay],
  )

  const discard = useCallback(
    async (ref: string) => {
      if (!key) return
      setEntries(await mutateQueue(key, (es) => discardEntry(es, ref)))
    },
    [key],
  )

  return { entries, unsent: unsentCount(entries), available, enqueue, retry, discard, replay }
}
