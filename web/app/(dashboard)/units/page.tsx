"use client"

import { useCallback, useState } from "react"
import { toast } from "sonner"
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

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

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

  const columns: ColumnDef<UnitDef>[] = [
    {
      id: "code",
      header: "代码",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.code}</span>,
    },
    { id: "name", header: "名称", cell: ({ row }) => row.original.name },
    {
      id: "type",
      header: "类型",
      cell: ({ row }) => (
        <span className="text-muted-foreground">{UNIT_TYPE_LABEL[row.original.unit_type]}</span>
      ),
    },
    {
      id: "source",
      header: "来源",
      cell: ({ row }) =>
        row.original.is_system ? (
          <Badge tone="neutral">系统</Badge>
        ) : (
          <Badge tone="accent">自定义</Badge>
        ),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) =>
        row.original.is_system ? (
          <span className="text-xs text-muted-foreground">—</span>
        ) : (
          <button
            type="button"
            onClick={() => handleDelete(row.original)}
            className="text-xs text-destructive hover:underline"
          >
            删除
          </button>
        ),
    },
  ]

  return (
    <PageContainer width="default">
      <PageHeader
        title="单位"
        subtitle="商品和单据使用的计量单位 — 系统内置不可删，自定义可增删。"
        actions={
          <Button
            onClick={() => {
              setShowCreate(true)
              setCreateError(null)
            }}
          >
            + 新建单位
          </Button>
        }
      />

      {showCreate && (
        <form onSubmit={handleCreate} className="mb-4 space-y-3 rounded-xl border border-border bg-card p-4">
          <h2 className="text-sm font-medium">新建单位</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label htmlFor="unit-code">代码 *</Label>
              <Input
                id="unit-code"
                required
                maxLength={32}
                value={form.code}
                onChange={(e) => setForm({ ...form, code: e.target.value })}
                placeholder="例如：carton"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="unit-name">名称 *</Label>
              <Input
                id="unit-name"
                required
                maxLength={64}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="例如：箱"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="unit-type">类型 *</Label>
              <select
                id="unit-type"
                value={form.unit_type}
                onChange={(e) => setForm({ ...form, unit_type: e.target.value as UnitType })}
                className={SELECT_CLASS}
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
        getRowId={(u) => u.id}
        skeletonRows={6}
        animateRows
        empty={
          <EmptyState
            title="暂无单位"
            description="新建一个自定义单位，或重新启用系统内置单位（pcs / kg 等）。"
          />
        }
      />
    </PageContainer>
  )
}
