"use client"

import { useCallback, useMemo, useState } from "react"
import { toast } from "sonner"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useConfirm } from "@/hooks/useConfirm"
import { useRole } from "@/hooks/use-role"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ErrorBanner } from "@/components/ui/error-banner"
import { TableSkeleton } from "@/components/ui/table-skeleton"
import { extractApiError } from "@/lib/api/errors"
import { qrMatrix, qrPath } from "@/lib/qr"
import {
  ASSIGNABLE_ROLES,
  createInvite,
  listInvites,
  listMembers,
  removeMember,
  revokeInvite,
  updateMemberRole,
  type CreatedInvite,
  type TeamInvite,
  type TeamMember,
  type TeamRole,
} from "@/lib/api/team"

const TTL_OPTIONS = [
  { hours: 24, label: "1 天" },
  { hours: 72, label: "3 天" },
  { hours: 168, label: "7 天" },
]

function formatDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN", { hour12: false })
}

function QrCode({ text }: { text: string }) {
  const svg = useMemo(() => {
    try {
      const m = qrMatrix(text)
      return { size: m.length, d: qrPath(m) }
    } catch {
      return null
    }
  }, [text])
  if (!svg) return null
  const quiet = 4
  const box = svg.size + quiet * 2
  return (
    <svg
      role="img"
      aria-label="邀请二维码"
      viewBox={`0 0 ${box} ${box}`}
      className="h-40 w-40 shrink-0 rounded-lg border border-border bg-white"
      shapeRendering="crispEdges"
    >
      <rect width={box} height={box} fill="#fff" />
      <path d={svg.d} transform={`translate(${quiet} ${quiet})`} fill="#000" />
    </svg>
  )
}

/**
 * 团队成员: members, role changes, removal and invite links. Owner/manager
 * only; the backend enforces the same rule, this view just explains it.
 */
export function TeamTab() {
  const { role: viewer, known, can } = useRole()
  const confirm = useConfirm()
  const allowed = known && can("team.manage")
  const viewerIsOwner = viewer === "owner"

  const [members, setMembers] = useState<TeamMember[] | null>(null)
  const [invites, setInvites] = useState<TeamInvite[]>([])
  const [error, setError] = useState<string | null>(null)
  const [cashierAllowed, setCashierAllowed] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)

  const [inviteRole, setInviteRole] = useState<TeamRole>("clerk")
  const [ttl, setTtl] = useState(72)
  const [created, setCreated] = useState<CreatedInvite | null>(null)

  const roleOptions = ASSIGNABLE_ROLES.filter(
    (r) => (viewerIsOwner || r.value !== "manager") && (cashierAllowed || r.value !== "cashier"),
  )
  // A member who already is a cashier keeps their current option so the dropdown shows the truth.
  const optionsFor = (m: TeamMember) =>
    m.role === "cashier" && !cashierAllowed
      ? ASSIGNABLE_ROLES.filter((r) => r.value === "cashier" || roleOptions.some((o) => o.value === r.value))
      : roleOptions

  const load = useCallback(
    (signal?: AbortSignal, isCancelled?: () => boolean) => {
      Promise.all([listMembers({ signal }), listInvites({ signal })])
        .then(([m, i]) => {
          if (isCancelled?.()) return
          setMembers(m.members)
          setCashierAllowed(m.cashierAllowed)
          setInvites(i)
          setError(null)
        })
        .catch((err: unknown) => {
          if (isCancelled?.()) return
          setError(extractApiError(err).message)
        })
    },
    [],
  )

  useAbortableEffect(
    (signal, isCancelled) => {
      if (allowed) load(signal, isCancelled)
    },
    [allowed, load],
  )

  if (!known) {
    return (
      <div className="px-6 py-6">
        <TableSkeleton rows={3} cols={2} />
      </div>
    )
  }

  if (!allowed) {
    return (
      <div className="px-6 py-6">
        <h1 className="mb-2 text-xl font-semibold">团队成员</h1>
        <p className="text-sm text-muted-foreground">只有老板或店长可以管理团队成员。</p>
      </div>
    )
  }

  // The owner row is fixed; a manager cannot touch other managers; nobody edits themselves here.
  function editable(m: TeamMember): boolean {
    if (m.is_self || m.role === "owner") return false
    if (!viewerIsOwner && m.role === "manager") return false
    return true
  }

  async function handleRole(m: TeamMember, role: TeamRole) {
    if (role === m.role) return
    const name = m.display_name || "该成员"
    const toLabel = ASSIGNABLE_ROLES.find((r) => r.value === role)?.label ?? ""
    const ok = await confirm({
      title: `把 ${name} 改为${toLabel}？`,
      body: `${m.role_label}改为${toLabel}后，对方能看到和能做的事会马上跟着变。`,
      confirmText: "确认修改",
      cancelText: "不改了",
    })
    if (!ok) return
    setBusy(`role-${m.account_id}`)
    try {
      await updateMemberRole(m.account_id, role)
      toast.success(`已把 ${name} 改为${toLabel}`)
      load()
    } catch (err) {
      toast.error(extractApiError(err).message)
      load()
    } finally {
      setBusy(null)
    }
  }

  async function handleRemove(m: TeamMember) {
    const ok = await confirm({
      title: `移出 ${m.display_name || "该成员"}？`,
      body: "移出后对方不能再进入这个店铺，需要重新邀请才能回来。",
      confirmText: "移出",
      cancelText: "保留",
      danger: true,
    })
    if (!ok) return
    setBusy(`rm-${m.account_id}`)
    try {
      await removeMember(m.account_id)
      toast.success("已移出")
      load()
    } catch (err) {
      toast.error(extractApiError(err).message)
    } finally {
      setBusy(null)
    }
  }

  async function handleCreate() {
    setBusy("create")
    try {
      const res = await createInvite(inviteRole, ttl)
      setCreated(res)
      load()
    } catch (err) {
      toast.error(extractApiError(err).message)
    } finally {
      setBusy(null)
    }
  }

  async function handleRevoke(iv: TeamInvite) {
    const ok = await confirm({
      title: "撤销这个邀请？",
      body: `撤销后，${iv.role_label}邀请链接立即失效。`,
      confirmText: "撤销",
      cancelText: "保留",
      danger: true,
    })
    if (!ok) return
    setBusy(`iv-${iv.id}`)
    try {
      await revokeInvite(iv.id)
      if (created?.invite_id === iv.id) setCreated(null)
      toast.success("已撤销")
      load()
    } catch (err) {
      toast.error(extractApiError(err).message)
    } finally {
      setBusy(null)
    }
  }

  async function copyLink(link: string) {
    try {
      await navigator.clipboard.writeText(link)
      toast.success("已复制邀请链接")
    } catch {
      toast.error("复制失败，请长按或选中链接后手动复制")
    }
  }

  const fullLink = created ? `${window.location.origin}${created.link}` : ""
  const selectCls = "h-8 rounded-md border border-input bg-background px-2 text-sm"

  return (
    <div className="px-6 py-6">
      <h1 className="mb-2 text-xl font-semibold">团队成员</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        {members === null
          ? "邀请成员加入你的店铺，并决定每个人能看到和做什么。"
          : cashierAllowed
          ? viewerIsOwner
            ? "邀请店长、店员、收银、财务加入你的店铺，并决定每个人能看到和做什么。"
            : "邀请店员、收银、财务加入你的店铺，并决定每个人能看到和做什么。"
          : viewerIsOwner
            ? "邀请店长、店员、财务加入你的店铺，并决定每个人能看到和做什么。"
            : "邀请店员、财务加入你的店铺，并决定每个人能看到和做什么。"}
      </p>
      {error && <ErrorBanner onDismiss={() => setError(null)}>{error}</ErrorBanner>}

      {members === null && !error && <TableSkeleton rows={3} cols={2} />}
      {members && (
        <ul className="divide-y divide-border rounded-xl border border-border bg-card">
          {members.map((m) => (
            <li key={m.account_id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">
                  {m.display_name || "未命名成员"}
                  {m.is_self && (
                    <span className="ml-2 inline-flex">
                      <Badge tone="ok">你</Badge>
                    </span>
                  )}
                </p>
                <p className="truncate text-xs text-muted-foreground">
                  {[m.email_masked, m.phone_masked].filter(Boolean).join(" · ") || "—"}
                  {m.joined_at ? ` · ${formatDate(m.joined_at)} 加入` : ""}
                </p>
              </div>
              {editable(m) ? (
                <div className="flex items-center gap-2">
                  <select
                    aria-label={`${m.display_name || "该成员"}的角色`}
                    className={selectCls}
                    value={m.role}
                    disabled={busy === `role-${m.account_id}`}
                    onChange={(e) => handleRole(m, e.target.value as TeamRole)}
                  >
                    {optionsFor(m).map((r) => (
                      <option key={r.value} value={r.value}>
                        {r.label}
                      </option>
                    ))}
                  </select>
                  {m.role_hint && <span className="text-xs text-amber-600">{m.role_hint}</span>}
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    aria-label={`移除${m.display_name || "该成员"}`}
                    disabled={busy === `rm-${m.account_id}`}
                    onClick={() => handleRemove(m)}
                  >
                    移除
                  </Button>
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  {m.role_hint && <span className="text-xs text-amber-600">{m.role_hint}</span>}
                  <Badge tone={m.role === "owner" ? "accent" : "neutral"}>{m.role_label}</Badge>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      {members && (
        <>
          <section className="mt-8">
            <h2 className="mb-2 text-sm font-medium">邀请新成员</h2>
            <div className="flex flex-wrap items-end gap-3">
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                角色
                <select
                  className={selectCls}
                  value={inviteRole}
                  onChange={(e) => setInviteRole(e.target.value as TeamRole)}
                >
                  {roleOptions.map((r) => (
                    <option key={r.value} value={r.value}>
                      {r.label}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                有效期
                <select className={selectCls} value={ttl} onChange={(e) => setTtl(Number(e.target.value))}>
                  {TTL_OPTIONS.map((o) => (
                    <option key={o.hours} value={o.hours}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </label>
              <Button type="button" size="sm" disabled={busy === "create"} onClick={handleCreate}>
                生成邀请链接
              </Button>
            </div>
            <p className="mt-2 text-xs text-muted-foreground">
              {ASSIGNABLE_ROLES.find((r) => r.value === inviteRole)?.hint}。每个链接只能用一次。
            </p>

            {created && (
              <div className="mt-4 flex flex-wrap items-start gap-4 rounded-xl border border-border bg-card p-4">
                <QrCode text={fullLink} />
                <div className="min-w-0 flex-1 space-y-2">
                  <p className="text-sm font-medium">
                    {created.role_label}邀请已生成，{formatDate(created.expires_at)} 前有效
                  </p>
                  <div className="flex gap-2">
                    <input
                      readOnly
                      aria-label="邀请链接"
                      value={fullLink}
                      onFocus={(e) => e.currentTarget.select()}
                      className="h-8 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-xs"
                    />
                    <Button type="button" size="sm" variant="outline" onClick={() => copyLink(fullLink)}>
                      复制
                    </Button>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    把链接或二维码发给对方，对方打开链接、登录后点「加入店铺」即可（开过店且已录入真实商品、单据的账号加入不了；只开了空店的账号可以在加入时放弃空店）。离开本页后链接不会再显示，需要的话请重新生成。
                  </p>
                  <Button type="button" size="sm" onClick={() => setCreated(null)}>
                    完成
                  </Button>
                </div>
              </div>
            )}
          </section>

          <section className="mt-8">
            <h2 className="mb-2 text-sm font-medium">待加入的邀请</h2>
            {invites.length === 0 ? (
              <p className="text-sm text-muted-foreground">现在没有等待加入的邀请。</p>
            ) : (
              <ul className="divide-y divide-border rounded-xl border border-border bg-card">
                {invites.map((iv) => (
                  <li key={iv.id} className="flex items-center justify-between gap-3 px-4 py-3">
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium">{iv.role_label}邀请</p>
                      <p className="text-xs text-muted-foreground">{formatDate(iv.expires_at)} 前有效</p>
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      aria-label={`撤销${iv.role_label}邀请`}
                      disabled={busy === `iv-${iv.id}`}
                      onClick={() => handleRevoke(iv)}
                    >
                      撤销
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </div>
  )
}
