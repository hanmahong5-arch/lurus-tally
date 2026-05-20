"use client"

import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useCallback, useState } from "react"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { EmptyState } from "@/components/ui/empty-state"
import {
  fetchAccountSummary,
  type AccountSummary,
  daysUntilExpiry,
} from "@/lib/api/account"
import { formatCNY } from "@/lib/format"
import { useAccountDrawer } from "@/components/account/account-drawer-provider"
import { cn } from "@/lib/utils"
import { SubscriptionPlansView } from "../subscription/plans-view"
import ApiKeysPage from "../settings/api-keys/page"

type TabKey =
  | "profile"
  | "subscription"
  | "wallet"
  | "security"
  | "audit"
  | "api-keys"
  | "team"
  | "notifications"

interface TabDef {
  key: TabKey
  label: string
  icon: string
}

const TABS: TabDef[] = [
  { key: "profile", label: "个人资料", icon: "👤" },
  { key: "subscription", label: "订阅 & 计费", icon: "💳" },
  { key: "wallet", label: "钱包流水", icon: "💰" },
  { key: "security", label: "安全", icon: "🛡️" },
  { key: "audit", label: "活动日志", icon: "📋" },
  { key: "api-keys", label: "API 密钥", icon: "🔑" },
  { key: "team", label: "团队成员", icon: "👥" },
  { key: "notifications", label: "通知偏好", icon: "🔔" },
]

function isTabKey(s: string | null): s is TabKey {
  return s !== null && TABS.some((t) => t.key === s)
}

/**
 * Full account center — left vertical tab nav + right content. ?tab=... is
 * the source of truth so refresh / deep-link / drawer-click all land on the
 * right pane.
 */
export function AccountCenter() {
  const router = useRouter()
  const params = useSearchParams()
  const { openDrawer } = useAccountDrawer()

  const initial = params.get("tab")
  const activeTab: TabKey = isTabKey(initial) ? initial : "profile"

  const setTab = useCallback(
    (next: TabKey) => {
      const q = new URLSearchParams(params.toString())
      q.set("tab", next)
      router.replace(`/account?${q.toString()}`, { scroll: false })
    },
    [params, router],
  )

  return (
    <div className="flex h-full">
      <aside className="hidden w-48 shrink-0 flex-col gap-1 border-r border-border bg-background p-3 md:flex">
        <button
          type="button"
          onClick={openDrawer}
          className="mb-3 rounded-lg border border-border bg-card px-3 py-2 text-left text-xs text-muted-foreground hover:bg-muted"
        >
          ← 返回摘要
        </button>
        {TABS.map((t) => (
          <button
            type="button"
            key={t.key}
            onClick={() => setTab(t.key)}
            data-testid={`account-tab-${t.key}`}
            className={cn(
              "flex items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors",
              t.key === activeTab
                ? "bg-muted font-medium text-foreground"
                : "text-muted-foreground hover:bg-muted hover:text-foreground",
            )}
          >
            <span aria-hidden="true">{t.icon}</span>
            {t.label}
          </button>
        ))}
      </aside>

      <div className="flex-1 overflow-y-auto">
        {/* Mobile tab dropdown */}
        <div className="border-b border-border bg-background p-3 md:hidden">
          <select
            value={activeTab}
            onChange={(e) => setTab(e.target.value as TabKey)}
            className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm"
            aria-label="选择标签"
          >
            {TABS.map((t) => (
              <option key={t.key} value={t.key}>
                {t.icon} {t.label}
              </option>
            ))}
          </select>
        </div>

        <div className="mx-auto max-w-4xl">
          <TabContent tab={activeTab} />
        </div>
      </div>
    </div>
  )
}

function TabContent({ tab }: { tab: TabKey }) {
  switch (tab) {
    case "profile":
      return <ProfileTab />
    case "subscription":
      return (
        <div className="px-6 py-6">
          <h1 className="mb-2 text-xl font-semibold">订阅 & 计费</h1>
          <p className="mb-6 text-sm text-muted-foreground">
            升级套餐或一键续费，钱包余额可即时开通；支付宝 / 微信跳转支付。
          </p>
          <SubscriptionPlansView />
        </div>
      )
    case "wallet":
      return <WalletTab />
    case "api-keys":
      return (
        // Reuse existing api-keys page. It already ships its own header / max-w
        // / new-key flow.
        <ApiKeysPage />
      )
    case "security":
      return (
        <PlaceholderTab
          title="安全"
          description="登录会话列表 / 撤销其他设备 / 修改密码 / 2FA —— Phase 3 上线"
        />
      )
    case "audit":
      return (
        <PlaceholderTab
          title="活动日志"
          description="审计流水（建单 / 审核 / 退款 / 密钥变更）—— Phase 3 上线"
        />
      )
    case "team":
      return (
        <PlaceholderTab
          title="团队成员"
          description="成员列表 / 邀请 / 角色 —— platform-core 接口对接中"
        />
      )
    case "notifications":
      return <NotificationsTab />
    default:
      return null
  }
}

function ProfileTab() {
  const [summary, setSummary] = useState<AccountSummary | null>(null)
  const [error, setError] = useState<string | null>(null)

  useAbortableEffect((_signal, isCancelled) => {
    fetchAccountSummary()
      .then((s) => !isCancelled() && setSummary(s))
      .catch((err: Error) => !isCancelled() && setError(err.message))
  }, [])

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">个人资料</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        基础身份信息来自 Zitadel SSO。修改 display_name / 手机 / 头像需 Phase 3 后端接口。
      </p>
      {error && (
        <p className="mb-4 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">
          加载失败：{error}
        </p>
      )}
      {summary && (
        <dl className="divide-y divide-border rounded-xl border border-border bg-card">
          <ProfileRow label="显示名" value={summary.identity.display_name || "—"} />
          <ProfileRow label="邮箱" value={summary.identity.email || "—"} />
          <ProfileRow label="角色" value={summary.identity.role || "—"} />
          <ProfileRow label="租户 ID" value={summary.identity.tenant_id || "—"} mono />
          <ProfileRow label="用户 ID" value={summary.identity.user_id || "—"} mono />
          <ProfileRow
            label="业务类型"
            value={summary.identity.profile_type || "未设置"}
          />
        </dl>
      )}
    </div>
  )
}

function ProfileRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-3 gap-4 px-4 py-3 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("col-span-2", mono && "font-mono text-xs break-all")}>{value}</dd>
    </div>
  )
}

function WalletTab() {
  const [summary, setSummary] = useState<AccountSummary | null>(null)
  const [error, setError] = useState<string | null>(null)

  useAbortableEffect((_signal, isCancelled) => {
    fetchAccountSummary()
      .then((s) => !isCancelled() && setSummary(s))
      .catch((err: Error) => !isCancelled() && setError(err.message))
  }, [])

  const wallet = summary?.billing?.wallet
  const sub = summary?.billing?.subscription
  const days = daysUntilExpiry(sub?.expires_at)

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">钱包流水</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        Tally 钱包由 lurus-platform 统一管理。明细流水 API 接入后将出现在下方。
      </p>
      {error && (
        <p className="mb-4 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">
          加载失败：{error}
        </p>
      )}
      {summary && (
        <div className="grid gap-4 sm:grid-cols-3">
          <BalanceCard label="可用余额" value={formatCNY(wallet?.available ?? 0)} tone="primary" />
          <BalanceCard label="冻结余额" value={formatCNY(wallet?.frozen ?? 0)} tone="muted" />
          <BalanceCard label="总额" value={formatCNY(wallet?.total ?? 0)} tone="muted" />
        </div>
      )}
      {sub && (
        <p className="mt-4 text-xs text-muted-foreground">
          当前套餐 <strong>{sub.plan_code}</strong>，状态 {sub.status}
          {days !== null && (days >= 0 ? `，剩 ${days} 天` : "，已到期")}
          。前往订阅页可一键续费或升级。
        </p>
      )}
      <div className="mt-6">
        <Link
          href="/account?tab=subscription"
          className="inline-flex rounded-md border border-border bg-card px-3 py-1.5 text-sm hover:bg-muted"
        >
          去订阅页 →
        </Link>
      </div>
      <div className="mt-8">
        <EmptyState
          title="流水明细即将上线"
          description="对账明细需要 platform-core 暴露 wallet/transactions 接口（Phase 3）。"
        />
      </div>
    </div>
  )
}

function BalanceCard({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone: "primary" | "muted"
}) {
  return (
    <div
      className={cn(
        "rounded-xl border border-border p-4",
        tone === "primary" ? "bg-primary/5" : "bg-card",
      )}
    >
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-2xl tabular-nums">{value}</div>
    </div>
  )
}

const NOTIF_KEY = "tally.notification_prefs"

interface NotifPrefs {
  low_stock: boolean
  bill_approved: boolean
  payment_received: boolean
  weekly_digest: boolean
}

const DEFAULT_PREFS: NotifPrefs = {
  low_stock: true,
  bill_approved: true,
  payment_received: false,
  weekly_digest: true,
}

const NOTIF_ROWS: { key: keyof NotifPrefs; label: string; hint: string }[] = [
  { key: "low_stock", label: "低库存预警", hint: "SKU 跌破安全库存时即时通知" },
  { key: "bill_approved", label: "单据审核", hint: "采购 / 销售单据被审核或拒绝" },
  { key: "payment_received", label: "收款到账", hint: "客户付款到账事件" },
  { key: "weekly_digest", label: "周报", hint: "每周一早 9 点发送本店经营摘要" },
]

function NotificationsTab() {
  const [prefs, setPrefs] = useState<NotifPrefs>(() => {
    if (typeof window === "undefined") return DEFAULT_PREFS
    try {
      const raw = window.localStorage.getItem(NOTIF_KEY)
      if (!raw) return DEFAULT_PREFS
      return { ...DEFAULT_PREFS, ...(JSON.parse(raw) as Partial<NotifPrefs>) }
    } catch {
      return DEFAULT_PREFS
    }
  })
  const [saved, setSaved] = useState(false)

  function toggle(key: keyof NotifPrefs) {
    const next = { ...prefs, [key]: !prefs[key] }
    setPrefs(next)
    try {
      window.localStorage.setItem(NOTIF_KEY, JSON.stringify(next))
      setSaved(true)
      setTimeout(() => setSaved(false), 1200)
    } catch {
      // localStorage unavailable — silently ignore.
    }
  }

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">通知偏好</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        当前为本地偏好（保存在浏览器）。后端通知服务接入后将与服务端同步。
      </p>
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {NOTIF_ROWS.map((row) => (
          <li key={row.key} className="flex items-center justify-between gap-4 px-4 py-3">
            <div>
              <div className="text-sm font-medium">{row.label}</div>
              <div className="text-xs text-muted-foreground">{row.hint}</div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={prefs[row.key]}
              onClick={() => toggle(row.key)}
              className={cn(
                "relative inline-flex h-6 w-11 items-center rounded-full transition-colors",
                prefs[row.key] ? "bg-primary" : "bg-muted",
              )}
            >
              <span
                className={cn(
                  "inline-block h-5 w-5 transform rounded-full bg-background shadow-sm transition-transform",
                  prefs[row.key] ? "translate-x-5" : "translate-x-0.5",
                )}
              />
            </button>
          </li>
        ))}
      </ul>
      {saved && <p className="mt-3 text-xs text-emerald-600">已保存</p>}
    </div>
  )
}

function PlaceholderTab({ title, description }: { title: string; description: string }) {
  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">{title}</h1>
      <p className="mb-6 text-sm text-muted-foreground">{description}</p>
      <EmptyState title="即将上线" description={description} />
    </div>
  )
}
