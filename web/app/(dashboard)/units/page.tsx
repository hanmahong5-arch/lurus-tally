"use client"

import { useCallback, useState } from "react"
import { toast } from "sonner"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useConfirm } from "@/hooks/useConfirm"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorBanner } from "@/components/ui/error-banner"
import { TableSkeleton } from "@/components/ui/table-skeleton"
import {
  createUnit,
  deleteUnit,
  listUnits,
  type CreateUnitInput,
  type UnitDef,
  type UnitType,
} from "@/lib/api/units"

const UNIT_TYPE_LABEL: Record<UnitType, string> = {
  count: "计数",
  weight: "重量",
  length: "长度",
  volume: "体积",
  area: "面积",
  time: "时间",
}

const UNIT_TYPES: UnitType[] = ["count", "weight", "length", "volume", "area", "time"]

/**
 * /units — unit catalogue. Tenant-level unit definitions used by product
 * pricing and bill line items. System units (is_system=true) are immutable.
 */
export default function UnitsPage() {
  const [items, setItems] = useState<UnitDef[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [showCreate, setShowCreate] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [form, setForm] = useState<CreateUnitInput>({ code: "", name: "", unit_type: "count" })

  const confirm = useConfirm()

  const load = useCallback(
    (signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listUnits()
        .then((res) => {
          if (isCancelled?.()) return
          setItems(res.items ?? [])
        })
        .catch((e: Error) => {
          if (isCancelled?.() || signal?.aborted) return
          setError(e.message)
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
    if (!form.code.trim() || !form.name.trim()) {
      setCreateError("代码和名称不能为空")
      return
    }
    setCreating(true)
    setCreateError(null)
    try {
      await createUnit({
        code: form.code.trim(),
        name: form.name.trim(),
        unit_type: form.unit_type,
      })
      toast.success("已创建单位")
      setForm({ code: "", name: "", unit_type: "count" })
      setShowCreate(false)
      load()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(unit: UnitDef) {
    if (unit.is_system) {
      toast.error("系统内置单位不可删除")
      return
    }
    const ok = await confirm({
      title: `删除 ${unit.name}？`,
      body: "已引用此单位的商品 / 单据会保留快照值，但新数据不能再选它。",
      confirmText: "删除",
      cancelText: "取消",
      danger: true,
    })
    if (!ok) return
    try {
      await deleteUnit(unit.id)
      toast.success("已删除")
      load()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="mx-auto max-w-4xl px-6 py-6">
      <header className="mb-4 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">单位</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">
            商品和单据使用的计量单位 — 系统内置不可删，自定义可增删。
          </p>
        </div>
        <button
          type="button"
          onClick={() => {
            setShowCreate(true)
            setCreateError(null)
          }}
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90"
        >
          + 新建单位
        </button>
      </header>

      {showCreate && (
        <form
          onSubmit={handleCreate}
          className="mb-4 space-y-3 rounded-xl border border-border bg-card p-4"
        >
          <h2 className="text-sm font-medium">新建单位</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="space-y-1">
              <label htmlFor="unit-code" className="text-xs text-muted-foreground">
                代码 *
              </label>
              <input
                id="unit-code"
                required
                maxLength={32}
                value={form.code}
                onChange={(e) => setForm({ ...form, code: e.target.value })}
                placeholder="例如：carton"
                className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="unit-name" className="text-xs text-muted-foreground">
                名称 *
              </label>
              <input
                id="unit-name"
                required
                maxLength={64}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="例如：箱"
                className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="unit-type" className="text-xs text-muted-foreground">
                类型 *
              </label>
              <select
                id="unit-type"
                value={form.unit_type}
                onChange={(e) => setForm({ ...form, unit_type: e.target.value as UnitType })}
                className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm"
              >
                {UNIT_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {UNIT_TYPE_LABEL[t]}
                  </option>
                ))}
              </select>
            </div>
          </div>
          {createError && <ErrorBanner>{createError}</ErrorBanner>}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setShowCreate(false)}
              disabled={creating}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-sm hover:bg-muted disabled:opacity-50"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={creating}
              className="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {creating ? "创建中..." : "创建"}
            </button>
          </div>
        </form>
      )}

      {loading && <TableSkeleton rows={6} />}
      {error && <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>}
      {!loading && !error && items.length === 0 && (
        <EmptyState
          title="暂无单位"
          description="新建一个自定义单位，或重新启用系统内置单位（pcs / kg 等）。"
        />
      )}
      {!loading && items.length > 0 && (
        <div className="overflow-x-auto rounded-xl border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 text-left font-medium">代码</th>
                <th className="px-4 py-2.5 text-left font-medium">名称</th>
                <th className="px-4 py-2.5 text-left font-medium">类型</th>
                <th className="px-4 py-2.5 text-left font-medium">来源</th>
                <th className="px-4 py-2.5 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {items.map((u) => (
                <tr key={u.id} className="hover:bg-muted/30">
                  <td className="px-4 py-2.5 font-mono text-xs">{u.code}</td>
                  <td className="px-4 py-2.5">{u.name}</td>
                  <td className="px-4 py-2.5 text-muted-foreground">
                    {UNIT_TYPE_LABEL[u.unit_type]}
                  </td>
                  <td className="px-4 py-2.5">
                    {u.is_system ? (
                      <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
                        系统
                      </span>
                    ) : (
                      <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] uppercase tracking-wide text-primary">
                        自定义
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    {u.is_system ? (
                      <span className="text-xs text-muted-foreground">—</span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => handleDelete(u)}
                        className="text-xs text-destructive hover:underline"
                      >
                        删除
                      </button>
                    )}
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
