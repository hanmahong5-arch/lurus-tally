"use client"

import { useCallback, useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useConfirm } from "@/hooks/useConfirm"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"
import {
  type PAT,
  type CreatedPAT,
  createPAT,
  listPATs,
  revokePAT,
} from "@/lib/api/pats"

const devTenantId = process.env.NEXT_PUBLIC_DEV_TENANT_ID

function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return "—"
  return new Date(iso).toLocaleString("zh-CN")
}

/**
 * /settings/api-keys — manage Personal Access Tokens (Phase 2b of ADR-0011).
 *
 * Plaintext token is shown EXACTLY ONCE in a banner immediately after creation
 * — server never echoes it again. The user must copy + store securely.
 */
export default function ApiKeysPage() {
  const [items, setItems] = useState<PAT[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [creating, setCreating] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState("")
  const [expiresAt, setExpiresAt] = useState("")
  const [createError, setCreateError] = useState<string | null>(null)
  const [newToken, setNewToken] = useState<CreatedPAT | null>(null)
  const [copied, setCopied] = useState(false)

  const confirm = useConfirm()

  const load = useCallback(
    (signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listPATs(devTenantId, signal)
        .then((res) => {
          if (isCancelled?.()) return
          setItems(res)
        })
        .catch((e) => {
          if (isCancelled?.() || signal?.aborted) return
          setError(String(e))
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

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setCreateError("名称不能为空")
      return
    }
    setCreating(true)
    setCreateError(null)
    try {
      const created = await createPAT(
        {
          name: trimmed,
          expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
        },
        devTenantId,
      )
      setNewToken(created)
      setShowCreate(false)
      setName("")
      setExpiresAt("")
      load()
    } catch (err) {
      setCreateError(String(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke(p: PAT) {
    const ok = await confirm({
      title: `吊销 ${p.name}？`,
      body: "吊销后使用此凭证的任何客户端将立即失去访问权限，且不可恢复。",
      confirmText: "确认吊销",
      cancelText: "保留",
      danger: true,
    })
    if (!ok) return
    try {
      await revokePAT(p.id, devTenantId)
      load()
    } catch (err) {
      setError(String(err))
    }
  }

  async function copyToken() {
    if (!newToken) return
    try {
      await navigator.clipboard.writeText(newToken.token)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // clipboard may be unavailable in insecure contexts — let user select manually
    }
  }

  return (
    <div className="p-6 max-w-4xl">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">API 密钥</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Personal Access Token —— 给 tally-mcp、Claude Desktop、OpenHuman、CI 脚本等外部客户端使用
          </p>
        </div>
        <button
          onClick={() => {
            setShowCreate(true)
            setCreateError(null)
            setNewToken(null)
          }}
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          + 新建密钥
        </button>
      </div>

      {/* One-time plaintext token banner */}
      {newToken && (
        <div className="mb-6 rounded-xl border border-amber-500/40 bg-amber-500/5 p-4">
          <div className="mb-2 flex items-center gap-2">
            <span aria-hidden="true">⚠️</span>
            <span className="font-medium">请立即复制并妥善保存</span>
          </div>
          <p className="mb-3 text-xs text-muted-foreground">
            「{newToken.name}」已创建。此令牌仅显示一次，关闭后无法再次查看 —— 服务器只保存哈希值。
          </p>
          <div className="flex gap-2">
            <input
              readOnly
              value={newToken.token}
              onFocus={(e) => e.currentTarget.select()}
              className="flex-1 rounded-md border border-input bg-background px-3 py-1.5 font-mono text-xs"
            />
            <button
              onClick={copyToken}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-xs hover:bg-muted transition-colors"
            >
              {copied ? "已复制" : "复制"}
            </button>
            <button
              onClick={() => setNewToken(null)}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-xs hover:bg-muted transition-colors"
            >
              我已保存
            </button>
          </div>
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <form
          onSubmit={handleCreate}
          className="mb-6 rounded-xl border border-border bg-card p-4 space-y-3"
        >
          <h2 className="text-sm font-medium">新建 Personal Access Token</h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="space-y-1">
              <label htmlFor="pat-name" className="text-xs text-muted-foreground">
                名称 <span className="text-destructive">*</span>
              </label>
              <input
                id="pat-name"
                type="text"
                required
                maxLength={64}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如：tally-mcp-laptop"
                className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="pat-expires" className="text-xs text-muted-foreground">
                过期时间（可选）
              </label>
              <input
                id="pat-expires"
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
          </div>
          {createError && <ErrorBanner>{createError}</ErrorBanner>}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setShowCreate(false)}
              disabled={creating}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-sm hover:bg-muted transition-colors disabled:opacity-50"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={creating}
              className="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {creating ? "创建中..." : "创建"}
            </button>
          </div>
        </form>
      )}

      {loading && (
        <div className="py-12 text-center text-muted-foreground">加载中...</div>
      )}
      {error && <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>}
      {!loading && !error && items.length === 0 && (
        <EmptyState
          title="暂无 API 密钥"
          description="为外部客户端创建第一把密钥 —— 例如本地运行 tally-mcp 接 Claude Desktop"
          action={
            <button
              onClick={() => {
                setShowCreate(true)
                setNewToken(null)
              }}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-sm hover:bg-muted transition-colors"
            >
              新建第一把密钥
            </button>
          }
        />
      )}

      {!loading && items.length > 0 && (
        <div className="overflow-x-auto rounded-xl border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 text-left font-medium">名称</th>
                <th className="px-4 py-2.5 text-left font-medium">前缀</th>
                <th className="px-4 py-2.5 text-left font-medium">权限</th>
                <th className="px-4 py-2.5 text-left font-medium">创建</th>
                <th className="px-4 py-2.5 text-left font-medium">上次使用</th>
                <th className="px-4 py-2.5 text-left font-medium">过期</th>
                <th className="px-4 py-2.5 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {items.map((p) => (
                <tr key={p.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-2.5 font-medium">{p.name}</td>
                  <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    tally_pat_{p.prefix}…
                  </td>
                  <td className="px-4 py-2.5">
                    {p.scopes.map((s) => (
                      <span
                        key={s}
                        className="mr-1 rounded-full bg-muted px-2 py-0.5 text-xs"
                      >
                        {s}
                      </span>
                    ))}
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground">{formatDateTime(p.created_at)}</td>
                  <td className="px-4 py-2.5 text-muted-foreground">{formatDateTime(p.last_used_at)}</td>
                  <td className="px-4 py-2.5 text-muted-foreground">
                    {p.expires_at ? formatDateTime(p.expires_at) : "无"}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <button
                      onClick={() => handleRevoke(p)}
                      className="text-xs text-destructive hover:underline"
                    >
                      吊销
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
