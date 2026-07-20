"use client"

import Link from "next/link"
import Image from "next/image"
import { useRouter, useSearchParams } from "next/navigation"
import { useCallback, useState } from "react"
import { toast } from "sonner"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useConfirm } from "@/hooks/useConfirm"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorBanner } from "@/components/ui/error-banner"
import { TableSkeleton } from "@/components/ui/table-skeleton"
import { Badge } from "@/components/ui/badge"
import {
  fetchAccountSummary,
  type AccountSummary,
  daysUntilExpiry,
} from "@/lib/api/account"
import {
  getProfile,
  listAuditLog,
  listSessions,
  revokeSession,
  updateProfile,
  uploadAvatar,
  type AccountSession,
  type AuditEntry,
} from "@/lib/api/account-extra"
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
      return <SecurityTab />
    case "audit":
      return <AuditTab />
    case "team":
      return <TeamTab />
    case "notifications":
      return <NotificationsTab />
    default:
      return null
  }
}

function ProfileTab() {
  const [summary, setSummary] = useState<AccountSummary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [displayName, setDisplayName] = useState("")
  const [phone, setPhone] = useState("")
  const [avatarUrl, setAvatarUrl] = useState<string>("")
  const [uploading, setUploading] = useState(false)

  const refresh = useCallback(() => {
    Promise.all([fetchAccountSummary(), getProfile().catch(() => null)])
      .then(([s, p]) => {
        setSummary(s)
        setDisplayName(p?.display_name || s.identity.display_name || "")
        setPhone(p?.phone || "")
        setAvatarUrl(toProxyUrl(p?.avatar_url || ""))
      })
      .catch((err: Error) => setError(err.message))
  }, [])

  useAbortableEffect((_signal, isCancelled) => {
    if (isCancelled()) return
    refresh()
  }, [refresh])

  async function handleSave() {
    setSaving(true)
    try {
      await updateProfile(displayName.trim(), phone.trim())
      toast.success("已保存")
      setEditing(false)
      refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  async function handleAvatarChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    if (file.size > 200 * 1024) {
      toast.error("文件超过 200KB")
      return
    }
    setUploading(true)
    try {
      const { avatar_url } = await uploadAvatar(file)
      setAvatarUrl(toProxyUrl(avatar_url) + "?t=" + Date.now())
      toast.success("头像已更新")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setUploading(false)
      e.target.value = ""
    }
  }

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">个人资料</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        显示名 / 手机 / 头像可在此修改；邮箱和角色由 SSO 身份服务管理。
      </p>
      {error && <ErrorBanner>加载失败：{error}</ErrorBanner>}

      {summary && (
        <div className="space-y-4">
          <div className="flex items-center gap-4 rounded-xl border border-border bg-card p-4">
            <div className="relative h-16 w-16 overflow-hidden rounded-full bg-muted">
              {avatarUrl ? (
                // unoptimized: avatarUrl is a same-origin session-cookie-gated proxy
                // URL — skip Next's optimizer re-fetch and just get the lazy-load +
                // reserved-size CLS guard that <Image> gives over a bare <img>.
                <Image src={avatarUrl} alt="头像" fill sizes="64px" unoptimized className="object-cover" />
              ) : (
                <div className="flex h-full w-full items-center justify-center text-xl font-medium uppercase">
                  {(displayName || summary.identity.email || "?")[0]?.toUpperCase() ?? "?"}
                </div>
              )}
            </div>
            <div className="flex-1">
              <label className="inline-block cursor-pointer rounded-md border border-border bg-background px-3 py-1.5 text-xs hover:bg-muted">
                {uploading ? "上传中..." : "上传头像"}
                <input
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  className="hidden"
                  onChange={handleAvatarChange}
                  disabled={uploading}
                />
              </label>
              <p className="mt-1 text-[10px] text-muted-foreground">PNG / JPEG / WebP · ≤200KB</p>
            </div>
          </div>

          <div className="rounded-xl border border-border bg-card">
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <h2 className="text-sm font-medium">基本信息</h2>
              {!editing ? (
                <button
                  type="button"
                  onClick={() => setEditing(true)}
                  className="rounded-md border border-border bg-background px-3 py-1 text-xs hover:bg-muted"
                >
                  编辑
                </button>
              ) : (
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      setEditing(false)
                      refresh()
                    }}
                    className="rounded-md border border-border bg-background px-3 py-1 text-xs hover:bg-muted"
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    onClick={handleSave}
                    disabled={saving}
                    className="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                  >
                    {saving ? "保存中..." : "保存"}
                  </button>
                </div>
              )}
            </div>
            <dl className="divide-y divide-border">
              <ProfileEditRow
                label="显示名"
                value={displayName}
                onChange={setDisplayName}
                editing={editing}
                fallback={summary.identity.display_name}
              />
              <ProfileEditRow
                label="手机"
                value={phone}
                onChange={setPhone}
                editing={editing}
                fallback="未填写"
              />
              <ProfileRow label="邮箱" value={summary.identity.email || "—"} />
              <ProfileRow label="角色" value={summary.identity.role || "—"} />
              <ProfileRow label="租户 ID" value={summary.identity.tenant_id || "—"} mono />
              <ProfileRow label="用户 ID" value={summary.identity.user_id || "—"} mono />
              <ProfileRow
                label="业务类型"
                value={summary.identity.profile_type || "未设置"}
              />
            </dl>
          </div>
        </div>
      )}
    </div>
  )
}

function ProfileEditRow({
  label,
  value,
  onChange,
  editing,
  fallback,
}: {
  label: string
  value: string
  onChange: (next: string) => void
  editing: boolean
  fallback: string
}) {
  return (
    <div className="grid grid-cols-3 gap-4 px-4 py-3 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="col-span-2">
        {editing ? (
          <input
            type="text"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            className="w-full rounded-md border border-input bg-transparent px-2 py-1 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        ) : (
          <span>{value || fallback || "—"}</span>
        )}
      </dd>
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
        <p className="mb-4 rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
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
      {saved && <p className="mt-3 text-xs text-success">已保存</p>}
    </div>
  )
}

function toProxyUrl(url: string): string {
  if (!url) return ""
  if (url.startsWith("/api/v1/")) return "/api/proxy" + url.slice("/api/v1".length)
  return url
}

function formatDateTime(iso: string): string {
  if (!iso) return "—"
  try {
    return new Date(iso).toLocaleString("zh-CN")
  } catch {
    return iso
  }
}

function SecurityTab() {
  const [sessions, setSessions] = useState<AccountSession[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const confirm = useConfirm()

  const load = useCallback(
    (signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listSessions(signal)
        .then((r) => {
          if (isCancelled?.()) return
          setSessions(r.items ?? [])
        })
        .catch((err: Error) => {
          if (isCancelled?.() || signal?.aborted) return
          setError(err.message)
        })
        .finally(() => {
          if (isCancelled?.()) return
          setLoading(false)
        })
    },
    [],
  )

  useAbortableEffect((signal, isCancelled) => {
    load(signal, isCancelled)
  }, [load])

  async function handleRevoke(s: AccountSession) {
    const ok = await confirm({
      title: s.current ? "撤销当前会话？" : "撤销该会话？",
      body: s.current
        ? "撤销后你需要重新登录。"
        : "该设备上未保存的工作将丢失，需要重新登录。",
      confirmText: "撤销",
      cancelText: "保留",
      danger: true,
    })
    if (!ok) return
    try {
      await revokeSession(s.id)
      toast.success("已撤销")
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">安全</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        登录会话列表。撤销其他设备会立即让那些浏览器失效；当前会话也可撤销（系统会要求重新登录）。
      </p>

      <section className="mb-6 rounded-xl border border-border bg-card p-4">
        <h2 className="mb-1 text-sm font-medium">密码 / 2FA</h2>
        <p className="mb-3 text-xs text-muted-foreground">
          密码和两步验证由 SSO 身份服务统一管理。点击下方按钮在 SSO 控制台修改。
        </p>
        <a
          href="https://auth.lurus.cn"
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex rounded-md border border-border bg-background px-3 py-1.5 text-xs hover:bg-muted"
        >
          打开 SSO 控制台 →
        </a>
      </section>

      <section>
        <h2 className="mb-3 text-sm font-medium">活动会话</h2>
        {loading && <TableSkeleton rows={3} />}
        {error && <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>}
        {!loading && !error && sessions.length === 0 && (
          <EmptyState title="无活动会话" description="没有其他登录设备 — 此页将在你下次登录后填充。" />
        )}
        {!loading && sessions.length > 0 && (
          <ul className="divide-y divide-border rounded-xl border border-border bg-card">
            {sessions.map((s) => (
              <li key={s.id} className="flex items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <p className="truncate text-sm font-medium">{s.user_agent || "未知设备"}</p>
                    {s.current && <Badge tone="ok">当前</Badge>}
                  </div>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {s.ip_addr ? `${s.ip_addr} · ` : ""}最近活动 {formatDateTime(s.last_active)}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => handleRevoke(s)}
                  className="text-xs text-destructive hover:underline"
                >
                  撤销
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}

const AUDIT_ACTION_LABEL: Record<string, string> = {
  "bill.created": "建单",
  "bill.approved": "审单通过",
  "bill.rejected": "审单拒绝",
  "alert.low_stock": "低库存预警",
  "alert.overstock": "高库存预警",
  "stock.movement_recorded": "库存变动",
  "stock.snapshot_updated": "库存快照更新",
  "ai.plan.executed": "AI 执行操作",
  "ai.plan.failed": "AI 操作失败",
}

function AuditTab() {
  const [items, setItems] = useState<AuditEntry[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const limit = 50

  useAbortableEffect((signal, isCancelled) => {
    setLoading(true)
    setError(null)
    listAuditLog(limit, offset, signal)
      .then((r) => {
        if (isCancelled()) return
        setItems(r.items ?? [])
        setTotal(r.total)
      })
      .catch((err: Error) => {
        if (isCancelled() || signal.aborted) return
        setError(err.message)
      })
      .finally(() => {
        if (isCancelled()) return
        setLoading(false)
      })
  }, [offset])

  const totalPages = Math.max(1, Math.ceil(total / limit))
  const currentPage = Math.floor(offset / limit) + 1

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">活动日志</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        审计流水（建单 / 审单 / 库存变动 / 预警事件）。最新事件在前。
      </p>

      {loading && <TableSkeleton rows={8} />}
      {error && <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>}
      {!loading && !error && items.length === 0 && (
        <EmptyState
          title="暂无审计记录"
          description="业务事件会在发生时自动出现在这里 — 通常审单 / 建单 / 预警都会留痕。"
        />
      )}
      {!loading && items.length > 0 && (
        <>
          <ul className="divide-y divide-border rounded-xl border border-border bg-card">
            {items.map((e) => (
              <li key={e.id} className="px-4 py-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">
                      {AUDIT_ACTION_LABEL[e.action] ?? e.action}
                    </p>
                    <p className="mt-0.5 truncate text-xs text-muted-foreground">
                      {e.actor_id || "system"}
                      {e.target_kind && e.target_id ? ` · ${e.target_kind}/${e.target_id}` : ""}
                    </p>
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {formatDateTime(e.created_at)}
                  </span>
                </div>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex items-center justify-between text-xs text-muted-foreground">
            <span>
              共 {total} 条 · 第 {currentPage} / {totalPages} 页
            </span>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setOffset(Math.max(0, offset - limit))}
                disabled={offset === 0}
                className="rounded-md border border-border bg-background px-2 py-1 hover:bg-muted disabled:opacity-40"
              >
                上一页
              </button>
              <button
                type="button"
                onClick={() => setOffset(offset + limit)}
                disabled={offset + limit >= total}
                className="rounded-md border border-border bg-background px-2 py-1 hover:bg-muted disabled:opacity-40"
              >
                下一页
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}

function TeamTab() {
  const [summary, setSummary] = useState<AccountSummary | null>(null)
  const [error, setError] = useState<string | null>(null)

  useAbortableEffect((_signal, isCancelled) => {
    fetchAccountSummary()
      .then((s) => !isCancelled() && setSummary(s))
      .catch((err: Error) => !isCancelled() && setError(err.message))
  }, [])

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">团队成员</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        多成员协作 (邀请 / 角色管理) 由 platform-core 接管 — 接口对接中，当前仅显示自己。
      </p>
      {error && <ErrorBanner>{error}</ErrorBanner>}
      {summary && (
        <ul className="divide-y divide-border rounded-xl border border-border bg-card">
          <li className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">
                {summary.identity.display_name || "—"}
                <span className="ml-2 inline-flex"><Badge tone="ok">你</Badge></span>
              </p>
              <p className="truncate text-xs text-muted-foreground">{summary.identity.email}</p>
            </div>
            <Badge tone="neutral">{summary.identity.role || "owner"}</Badge>
          </li>
        </ul>
      )}
      <p className="mt-4 text-xs text-muted-foreground">
        邀请功能上线后会在此卡片下方出现 “+ 邀请成员” 按钮。
      </p>
    </div>
  )
}
