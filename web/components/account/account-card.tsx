"use client"

import { useState } from "react"
import Image from "next/image"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import {
  computeStatusLight,
  fetchAccountSummary,
  type AccountStatusLight,
  type AccountSummary,
} from "@/lib/api/account"
import { useAccountDrawer } from "./account-drawer-provider"
import { cn } from "@/lib/utils"

const STATUS_TONE: Record<AccountStatusLight, { dot: string; label: string }> = {
  green: { dot: "bg-emerald-500", label: "正常" },
  amber: { dot: "bg-amber-500", label: "需关注" },
  red: { dot: "bg-red-500", label: "已到期" },
}

// The backend exposes avatars at /api/v1/account/avatar; the browser must hit
// the /api/proxy bridge so the session cookie translates into a Bearer token.
function avatarProxyUrl(rawUrl: string): string {
  if (!rawUrl) return ""
  if (rawUrl.startsWith("/api/v1/")) {
    return "/api/proxy" + rawUrl.slice("/api/v1".length)
  }
  return rawUrl
}

/**
 * AccountCard — Tier 1 of the account-center progression. Lives at the bottom
 * of the sidebar, ~64px tall, click-to-open the Tier 2 drawer.
 *
 * Soft-fails: when /me or /billing/overview throws, falls back to "未登录" but
 * never errors — the sidebar must never go blank for a logged-in user.
 */
export function AccountCard() {
  const [summary, setSummary] = useState<AccountSummary | null>(null)
  const [failed, setFailed] = useState(false)
  const { openDrawer } = useAccountDrawer()

  useAbortableEffect((_signal, isCancelled) => {
    fetchAccountSummary()
      .then((s) => {
        if (isCancelled()) return
        setSummary(s)
      })
      .catch(() => {
        if (isCancelled()) return
        setFailed(true)
      })
  }, [])

  const light: AccountStatusLight = summary ? computeStatusLight(summary.billing) : "amber"
  const tone = STATUS_TONE[light]
  const displayName = summary?.identity.display_name || summary?.identity.email || "账户"
  const planCode = summary?.billing?.subscription?.plan_code ?? "free"

  const avatarSrc = summary?.identity.avatar_url
    ? avatarProxyUrl(summary.identity.avatar_url)
    : null

  return (
    <button
      type="button"
      onClick={openDrawer}
      aria-label="账户摘要"
      className="mt-auto flex items-center gap-3 rounded-lg border border-border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted"
    >
      <div className="relative h-9 w-9 shrink-0 overflow-hidden rounded-full bg-muted text-sm font-medium uppercase">
        {avatarSrc ? (
          // unoptimized: avatarSrc is a same-origin session-cookie-gated proxy
          // URL — skip Next's optimizer re-fetch (which can't forward the
          // browser's cookie) and just get the lazy-load + reserved-size CLS
          // guard that <Image> gives over a bare <img>.
          <Image src={avatarSrc} alt="" fill sizes="36px" unoptimized className="object-cover" />
        ) : (
          <span className="flex h-full w-full items-center justify-center">
            {(displayName[0] ?? "?").toUpperCase()}
          </span>
        )}
        <span
          className={cn(
            "absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full ring-2 ring-background",
            tone.dot,
          )}
          aria-label={tone.label}
        />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">
          {failed ? "未登录" : displayName}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5">
          <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
            {planCode}
          </span>
          <span className="text-[10px] text-muted-foreground">{tone.label}</span>
        </div>
      </div>
      <span className="text-muted-foreground" aria-hidden="true">
        →
      </span>
    </button>
  )
}
