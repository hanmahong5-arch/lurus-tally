"use client"

import { useCallback, useState } from "react"
import type { ColumnDef } from "@tanstack/react-table"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useConfirm } from "@/hooks/useConfirm"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable } from "@/components/ui/data-table"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"
import { type PAT, type CreatedPAT, createPAT, listPATs, revokePAT } from "@/lib/api/pats"

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

  const columns: ColumnDef<PAT>[] = [
    {
      id: "name",
      header: "名称",
      cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
    },
    {
      id: "prefix",
      header: "前缀",
      cell: ({ row }) => (
        <span className="font-mono text-xs text-muted-foreground">tally_pat_{row.original.prefix}…</span>
      ),
    },
    {
      id: "scopes",
      header: "权限",
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          {row.original.scopes.map((s) => (
            <Badge key={s} tone="neutral">
              {s}
            </Badge>
          ))}
        </div>
      ),
    },
    {
      id: "created",
      header: "创建",
      cell: ({ row }) => <span className="text-muted-foreground">{formatDateTime(row.original.created_at)}</span>,
    },
    {
      id: "last_used",
      header: "上次使用",
      cell: ({ row }) => (
        <span className="text-muted-foreground">{formatDateTime(row.original.last_used_at)}</span>
      ),
    },
    {
      id: "expires",
      header: "过期",
      cell: ({ row }) => (
        <span className="text-muted-foreground">
          {row.original.expires_at ? formatDateTime(row.original.expires_at) : "无"}
        </span>
      ),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <button
          type="button"
          onClick={() => handleRevoke(row.original)}
          className="text-xs text-destructive hover:underline"
        >
          吊销
        </button>
      ),
    },
  ]

  return (
    <PageContainer width="default">
      <PageHeader
        title="API 密钥"
        subtitle="Personal Access Token —— 给 tally-mcp、Claude Desktop、OpenHuman、CI 脚本等外部客户端使用"
        actions={
          <Button
            onClick={() => {
              setShowCreate(true)
              setCreateError(null)
              setNewToken(null)
            }}
          >
            + 新建密钥
          </Button>
        }
      />

      {/* One-time plaintext token banner */}
      {newToken && (
        <div className="mb-6 rounded-xl border border-warning/40 bg-warning/10 p-4">
          <div className="mb-2 flex items-center gap-2">
            <span aria-hidden="true">⚠️</span>
            <span className="font-medium">请立即复制并妥善保存</span>
          </div>
          <p className="mb-3 text-xs text-muted-foreground">
            「{newToken.name}」已创建。此令牌仅显示一次，关闭后无法再次查看 —— 服务器只保存哈希值。
          </p>
          <div className="flex gap-2">
            <Input
              readOnly
              value={newToken.token}
              onFocus={(e) => e.currentTarget.select()}
              className="flex-1 font-mono text-xs"
            />
            <Button variant="outline" size="sm" onClick={copyToken}>
              {copied ? "已复制" : "复制"}
            </Button>
            <Button variant="outline" size="sm" onClick={() => setNewToken(null)}>
              我已保存
            </Button>
          </div>
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <form onSubmit={handleCreate} className="mb-6 space-y-3 rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium">新建 Personal Access Token</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="pat-name">
                名称 <span className="text-destructive">*</span>
              </Label>
              <Input
                id="pat-name"
                type="text"
                required
                maxLength={64}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如：tally-mcp-laptop"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pat-expires">过期时间（可选）</Label>
              <Input
                id="pat-expires"
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
              />
            </div>
          </div>
          {createError && <ErrorBanner>{createError}</ErrorBanner>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" size="sm" disabled={creating} onClick={() => setShowCreate(false)}>
              取消
            </Button>
            <Button type="submit" size="sm" disabled={creating}>
              {creating ? "创建中..." : "创建"}
            </Button>
          </div>
        </form>
      )}

      <DataTable
        columns={columns}
        data={items}
        loading={loading}
        error={error}
        getRowId={(p) => p.id}
        animateRows
        empty={
          <EmptyState
            title="暂无 API 密钥"
            description="为外部客户端创建第一把密钥 —— 例如本地运行 tally-mcp 接 Claude Desktop"
            action={
              <Button
                variant="outline"
                onClick={() => {
                  setShowCreate(true)
                  setNewToken(null)
                }}
              >
                新建第一把密钥
              </Button>
            }
          />
        }
      />
    </PageContainer>
  )
}
