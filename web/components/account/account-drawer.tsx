"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { signOut } from "next-auth/react"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import {
  computeStatusLight,
  daysUntilExpiry,
  fetchAccountSummary,
  type AccountSummary,
} from "@/lib/api/account"
import { formatCNY } from "@/lib/format"
import { useAccountDrawer } from "./account-drawer-provider"
import { cn } from "@/lib/utils"

// Hard-coded fallback labels for entitlements with known keys. Unknown keys are
// rendered with their raw key as label (still useful) instead of being hidden,
// because hiding silently makes "what am I paying for" invisible.
const ENTITLEMENT_LABEL: Record<string, string> = {
  sku_limit: "SKU 数",
  monthly_bill_limit: "月单据数",
  ai_call_limit: "AI 调用数",
  warehouse_limit: "仓库数",
  user_seat_limit: "成员席位",
}

const TOP_KEYS = ["sku_limit", "monthly_bill_limit", "ai_call_limit"] as const

const STATUS_LIGHT_CLASS = {
  green: "bg-emerald-500",
  amber: "bg-amber-500",
  red: "bg-red-500",
} as const

/**
 * AccountDrawer — Tier 2 of the account center. A 400px right-side panel with
 * identity / subscription / wallet / quotas cards plus shortcuts. Every card
 * is click-through to the matching Tier 3 tab at /account.
 *
 * Open/close state lives in AccountDrawerProvider so the sidebar card (Tier 1)
 * and the ⌘K palette can both trigger it without prop drilling.
 */
export function AccountDrawer() {
  const { open, closeDrawer } = useAccountDrawer()
  const [summary, setSummary] = useState<AccountSummary | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  // Refetch every time the drawer opens — billing state can change while the
  // user is on the page (e.g. plan switch in another tab).
  useAbortableEffect(
    (_signal, isCancelled) => {
      if (!open) return
      setLoadError(null)
      fetchAccountSummary()
        .then((s) => {
          if (isCancelled()) return
          setSummary(s)
        })
        .catch((err: Error) => {
          if (isCancelled()) return
          setLoadError(err.message)
        })
    },
    [open],
  )

  // ESC closes.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeDrawer()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, closeDrawer])

  async function copyTenantId() {
    if (!summary?.identity.tenant_id) return
    try {
      await navigator.clipboard.writeText(summary.identity.tenant_id)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard API unavailable (insecure context) — silently ignore.
    }
  }

  return (
    <>
      {open && (
        <div
          className="fixed inset-0 z-40 bg-black/30 backdrop-blur-sm"
          onClick={closeDrawer}
          aria-hidden="true"
        />
      )}
      <aside
        role="dialog"
        aria-modal="true"
        aria-label="账户摘要"
        data-testid="account-drawer"
        className={cn(
          "fixed right-0 top-0 z-50 flex h-full w-full max-w-[400px] flex-col bg-background shadow-2xl transition-transform duration-300",
          open ? "translate-x-0" : "translate-x-full",
        )}
      >
        <header className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold">账户摘要</h2>
          <button
            type="button"
            onClick={closeDrawer}
            aria-label="关闭"
            className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            ✕
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-4 py-4">
          {loadError && (
            <p className="mb-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">
              加载账户信息失败：{loadError}
            </p>
          )}
          {!summary && !loadError && (
            <p className="text-sm text-muted-foreground">加载中...</p>
          )}
          {summary && (
            <div className="space-y-4">
              <IdentityCard summary={summary} onCopy={copyTenantId} copied={copied} />
              <SubscriptionCard summary={summary} />
              <WalletCard summary={summary} />
              <EntitlementsCard summary={summary} />
            </div>
          )}
        </div>

        <footer className="space-y-2 border-t border-border px-4 py-3">
          <Link
            href="/account"
            onClick={closeDrawer}
            className="block w-full rounded-md border border-border bg-card px-3 py-2 text-center text-xs hover:bg-muted"
          >
            查看完整账户中心 →
          </Link>
          <button
            type="button"
            onClick={() => signOut({ callbackUrl: "/login" })}
            className="block w-full rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2 text-center text-xs text-destructive hover:bg-destructive/10"
          >
            退出登录
          </button>
        </footer>
      </aside>
    </>
  )
}

function IdentityCard({
  summary,
  onCopy,
  copied,
}: {
  summary: AccountSummary
  onCopy: () => void
  copied: boolean
}) {
  const { identity, billing } = summary
  const initial = (identity.display_name || identity.email || "?")[0]?.toUpperCase() ?? "?"
  const tenantShort = identity.tenant_id ? identity.tenant_id.slice(0, 8) : "—"
  const light = computeStatusLight(billing)
  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <div className="flex items-center gap-3">
        <div className="relative flex h-12 w-12 items-center justify-center rounded-full bg-muted text-base font-medium uppercase">
          {initial}
          <span
            className={cn(
              "absolute -bottom-0.5 -right-0.5 h-3 w-3 rounded-full ring-2 ring-background",
              STATUS_LIGHT_CLASS[light],
            )}
            aria-hidden="true"
          />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{identity.display_name || "未命名"}</div>
          <div className="truncate text-xs text-muted-foreground">{identity.email || "—"}</div>
        </div>
      </div>
      <div className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
        <span>租户</span>
        <code className="rounded bg-muted px-1.5 py-0.5 font-mono">{tenantShort}</code>
        <button
          type="button"
          onClick={onCopy}
          className="ml-auto rounded px-1.5 py-0.5 text-[10px] hover:bg-muted"
          title={identity.tenant_id}
        >
          {copied ? "已复制" : "复制"}
        </button>
      </div>
    </div>
  )
}

function SubscriptionCard({ summary }: { summary: AccountSummary }) {
  const sub = summary.billing?.subscription
  const days = daysUntilExpiry(sub?.expires_at)
  const expiryLabel =
    days === null
      ? "永久"
      : days < 0
        ? "已到期"
        : days <= 7
          ? `剩 ${days} 天`
          : `剩 ${days} 天`
  return (
    <Link
      href="/account?tab=subscription"
      className="block rounded-xl border border-border bg-card p-4 transition-colors hover:bg-muted/50"
    >
      <div className="flex items-start justify-between">
        <div>
          <div className="text-xs text-muted-foreground">当前套餐</div>
          <div className="mt-1 flex items-center gap-2">
            <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium uppercase text-primary">
              {sub?.plan_code ?? "free"}
            </span>
            <span className="text-xs text-muted-foreground">{sub?.status ?? "未激活"}</span>
          </div>
          <div className="mt-1 text-xs text-muted-foreground">{expiryLabel}</div>
        </div>
        <span className="rounded-md border border-border bg-background px-2 py-1 text-xs">升级 →</span>
      </div>
    </Link>
  )
}

function WalletCard({ summary }: { summary: AccountSummary }) {
  const wallet = summary.billing?.wallet
  const available = wallet?.available ?? 0
  const frozen = wallet?.frozen ?? 0
  const total = wallet?.total ?? available + frozen
  return (
    <Link
      href="/account?tab=wallet"
      className="block rounded-xl border border-border bg-card p-4 transition-colors hover:bg-muted/50"
    >
      <div className="flex items-start justify-between">
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">钱包余额</div>
          <div className="mt-1 font-mono text-lg tabular-nums">{formatCNY(available)}</div>
          <div className="mt-0.5 text-[10px] text-muted-foreground">
            冻结 {formatCNY(frozen)} · 总额 {formatCNY(total)}
          </div>
        </div>
        <span className="rounded-md border border-border bg-background px-2 py-1 text-xs">充值 →</span>
      </div>
    </Link>
  )
}

function EntitlementsCard({ summary }: { summary: AccountSummary }) {
  const ent = summary.billing?.entitlements ?? {}
  // Pick top 3 known keys that are actually present. Unknown keys → hide
  // silently per design (the drawer is a summary, not an audit).
  const rows = TOP_KEYS.map((key) => ({
    key,
    label: ENTITLEMENT_LABEL[key] ?? key,
    value: ent[key],
  })).filter((r) => r.value !== undefined)

  if (rows.length === 0) {
    return (
      <Link
        href="/account?tab=subscription"
        className="block rounded-xl border border-border bg-card p-4 transition-colors hover:bg-muted/50"
      >
        <div className="text-xs text-muted-foreground">配额</div>
        <p className="mt-1 text-xs text-muted-foreground">
          升级后可查看 SKU / 单据 / AI 调用配额。
        </p>
      </Link>
    )
  }
  return (
    <Link
      href="/account?tab=subscription"
      className="block rounded-xl border border-border bg-card p-4 transition-colors hover:bg-muted/50"
    >
      <div className="text-xs text-muted-foreground">配额</div>
      <ul className="mt-2 space-y-1.5 text-xs">
        {rows.map((r) => (
          <li key={r.key} className="flex justify-between gap-2">
            <span className="text-muted-foreground">{r.label}</span>
            <span className="font-mono tabular-nums">{r.value}</span>
          </li>
        ))}
      </ul>
    </Link>
  )
}
