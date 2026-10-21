"use client"

import Link from "next/link"

import { SEED_TRIAL } from "@/lib/billing/plans"

/**
 * AINotConfigured is the friendly empty state shown when the AI assistant
 * cannot be used: the service has none wired (`ai_not_configured`) or the
 * shop's plan does not include it (`ai_not_entitled`). It replaces the chat
 * area — no error bubble, no retry loop — and tells the shop owner what the
 * assistant does, how to get it, and that search still works.
 */
export function AINotConfigured({ reason = "not_configured" }: { reason?: "not_configured" | "not_entitled" }) {
  const notEntitled = reason === "not_entitled"
  return (
    <div
      data-testid="ai-not-configured"
      data-reason={reason}
      role="status"
      className="flex flex-1 flex-col items-center justify-center px-5 py-8"
    >
      <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-muted text-3xl" aria-hidden="true">
        ✨
      </div>
      <h3 className="mt-4 text-center text-base font-semibold text-foreground">AI 助手还没开通</h3>
      <p className="mt-1.5 max-w-xs text-center text-sm text-muted-foreground">
        开通后，你可以直接用大白话问库存和销售，比如“哪些商品快断货了”“这个月卖得最好的是什么”，
        它会帮你查数据、起草进货单。
      </p>
      {notEntitled && SEED_TRIAL ? (
        <p className="mt-3 max-w-xs text-center text-sm text-muted-foreground">试用期暂未开放</p>
      ) : notEntitled ? (
        <>
          <p className="mt-3 max-w-xs text-center text-sm text-muted-foreground">
            你现在的套餐不包含 AI 助手，升级套餐后就能用。
          </p>
          <Link
            href="/subscription"
            data-testid="ai-upgrade-link"
            className="mt-3 inline-flex h-9 items-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground hover:opacity-90"
          >
            了解升级
          </Link>
        </>
      ) : (
        <p className="mt-3 max-w-xs text-center text-sm text-muted-foreground">
          想开通？请联系你的服务商或管理员。
        </p>
      )}
      <p className="mt-3 max-w-xs rounded-lg bg-muted/60 px-3 py-2 text-center text-xs text-muted-foreground">
        现在也可以按 <kbd className="rounded border border-border bg-background px-1 font-mono">⌘K</kbd>
        （Windows 按 Ctrl+K）搜索商品、客户和单据，不受影响。
      </p>
    </div>
  )
}
