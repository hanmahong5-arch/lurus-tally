"use client"

import { useState } from "react"
import { useConfirm } from "@/hooks/useConfirm"
import { offlineRefOf, unsentCount, type QueueEntry, type QueueStatus } from "@/lib/pos/checkout-queue"

interface PendingCheckoutsProps {
  entries: QueueEntry[]
  onRetry: (clientRef: string) => void
  onDiscard: (clientRef: string) => void
  /** 直接铺开列表（今日结单的离线视图用），不要角标按钮。 */
  inline?: boolean
}

const STATUS_LABEL: Record<QueueStatus, string> = {
  pending: "待上传",
  sending: "上传中",
  failed: "上传失败",
  done: "已上传",
}

function formatTime(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/**
 * 收银头部角标「待上传 n」：点开看离线结账的单号、金额、时间、状态与错误。
 * 上传失败的单可以重试或放弃（放弃要确认）。没有任何条目时不占位。
 */
export function PendingCheckouts({ entries, onRetry, onDiscard, inline = false }: PendingCheckoutsProps) {
  const [open, setOpen] = useState(false)
  const confirm = useConfirm()
  if (entries.length === 0) return null
  const unsent = unsentCount(entries)

  async function handleDiscard(e: QueueEntry) {
    const ok = await confirm({
      title: "放弃这笔离线结账",
      body: `${offlineRefOf(e.client_ref)} 将不会上传，这笔销售不会记入账上，也不会扣库存。确定放弃？`,
      confirmText: "放弃",
      cancelText: "先留着",
      danger: true,
    })
    if (ok) onDiscard(e.client_ref)
  }

  const shown = open || inline
  return (
    <div className={inline ? "" : "relative"}>
      {!inline && <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className={`inline-flex min-h-[36px] items-center rounded-md border px-3 py-1.5 text-xs transition-colors hover:bg-muted ${
          unsent > 0 ? "border-amber-500 text-amber-700" : "border-border text-muted-foreground"
        }`}
      >
        {unsent > 0 ? `待上传 ${unsent}` : "离线单已上传"}
      </button>}
      {shown && (
        <div
          role={inline ? "region" : "dialog"}
          aria-label="离线结账列表"
          className={
            inline
              ? "rounded-lg border border-border bg-background p-2"
              : "absolute left-0 top-full z-30 mt-1 w-80 max-w-[90vw] rounded-lg border border-border bg-background p-2 shadow-lg"
          }
        >
          <ul className={inline ? "space-y-2" : "max-h-72 space-y-2 overflow-y-auto"}>
            {entries.map((e) => (
              <li key={e.client_ref} className="rounded-md border border-border px-3 py-2 text-xs">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono">{offlineRefOf(e.client_ref)}</span>
                  <span className={e.status === "failed" ? "text-destructive" : "text-muted-foreground"}>
                    {STATUS_LABEL[e.status]}
                  </span>
                </div>
                <div className="mt-1 flex items-center justify-between gap-2 text-muted-foreground">
                  <span>{formatTime(e.occurred_at)}</span>
                  {e.total_amount && <span className="font-mono">¥{e.total_amount}</span>}
                </div>
                {e.status === "done" && e.server_bill_no && (
                  <div className="mt-1 text-muted-foreground">正式单号 {e.server_bill_no}</div>
                )}
                {e.last_error && e.status !== "done" && (
                  <div className="mt-1 text-destructive">{e.last_error}</div>
                )}
                {e.status === "failed" && (
                  <div className="mt-2 flex gap-2">
                    <button
                      type="button"
                      onClick={() => onRetry(e.client_ref)}
                      className="rounded-md border border-border px-2 py-1 hover:bg-muted"
                    >
                      重试
                    </button>
                    <button
                      type="button"
                      onClick={() => void handleDiscard(e)}
                      className="rounded-md border border-border px-2 py-1 text-destructive hover:bg-muted"
                    >
                      放弃
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
