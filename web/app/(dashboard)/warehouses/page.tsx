"use client"

import { useCallback, useRef, useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  listWarehouses,
  deleteWarehouse,
  restoreWarehouse,
  createWarehouse,
  updateWarehouse,
  type WarehouseItem,
  type WarehouseCreateInput,
} from "@/lib/api/warehouses"
import { useConfirm } from "@/hooks/useConfirm"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable } from "@/components/ui/data-table"
import { Pagination } from "@/components/ui/pagination"
import { Sheet } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { EmptyState } from "@/components/ui/empty-state"

const PAGE_SIZE = 20

interface WarehouseForm {
  name: string
  code: string
  manager: string
  address: string
  remark: string
}

const TEXT_FIELDS: { key: keyof WarehouseForm; label: string; required?: boolean; placeholder: string }[] = [
  { key: "name", label: "名称", required: true, placeholder: "仓库名称" },
  { key: "code", label: "编号", placeholder: "可选" },
  { key: "manager", label: "负责人", placeholder: "可选" },
  { key: "address", label: "地址", placeholder: "可选" },
  { key: "remark", label: "备注", placeholder: "可选" },
]

const EMPTY_FORM: WarehouseForm = {
  name: "",
  code: "",
  manager: "",
  address: "",
  remark: "",
}

/**
 * WarehousesPage renders the warehouse list with search, pagination, and
 * a create/edit/view drawer (W3.D1).
 */
export default function WarehousesPage() {
  const [items, setItems] = useState<WarehouseItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [offset, setOffset] = useState(0)

  const [drawerItem, setDrawerItem] = useState<WarehouseItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">("view")
  const [showDrawer, setShowDrawer] = useState(false)
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState<WarehouseForm>(EMPTY_FORM)
  const [formIsDefault, setFormIsDefault] = useState(false)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const confirm = useConfirm()

  const load = useCallback(
    (query: string, off: number, signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listWarehouses({ q: query || undefined, limit: PAGE_SIZE, offset: off, signal, retry: 2 })
        .then((res) => {
          if (isCancelled?.()) return
          setItems(res.items ?? [])
          setTotal(res.total)
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
    []
  )

  useAbortableEffect((signal, isCancelled) => {
    load(q, offset, signal, isCancelled)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [offset])

  function handleSearchChange(value: string) {
    setQ(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      setOffset(0)
      load(value, 0)
    }, 300)
  }

  function openCreate() {
    setDrawerItem(null)
    setDrawerMode("create")
    setForm(EMPTY_FORM)
    setFormIsDefault(false)
    setShowDrawer(true)
  }

  function openDetail(item: WarehouseItem) {
    setDrawerItem(item)
    setDrawerMode("view")
    setShowDrawer(true)
  }

  function openEdit(item: WarehouseItem) {
    setDrawerItem(item)
    setDrawerMode("edit")
    setForm({
      name: item.name,
      code: item.code,
      manager: item.manager,
      address: item.address,
      remark: item.remark,
    })
    setFormIsDefault(item.isDefault)
    setShowDrawer(true)
  }

  function closeDrawer() {
    setShowDrawer(false)
    setDrawerItem(null)
  }

  async function handleSave() {
    if (!form.name.trim()) {
      toast.error("仓库名称不能为空")
      return
    }
    setSaving(true)
    try {
      const input: WarehouseCreateInput = {
        name: form.name.trim(),
        code: form.code.trim() || undefined,
        address: form.address.trim() || undefined,
        manager: form.manager.trim() || undefined,
        isDefault: formIsDefault,
        remark: form.remark.trim() || undefined,
      }
      if (drawerMode === "create") {
        await createWarehouse(input)
        toast.success("仓库已创建")
      } else if (drawerItem) {
        await updateWarehouse(drawerItem.id, input)
        toast.success("仓库已更新")
      }
      closeDrawer()
      load(q, offset)
    } catch (e) {
      toast.error("保存失败：" + String(e))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(item: WarehouseItem) {
    const ok = await confirm({
      title: "软删除仓库",
      body: `确认软删除「${item.name}」？删除后可在列表中恢复。`,
      confirmText: "删除",
      danger: true,
    })
    if (!ok) return
    try {
      await deleteWarehouse(item.id)
      load(q, offset)
    } catch (e) {
      toast.error("删除失败：" + String(e))
    }
  }

  async function handleRestore(id: string) {
    try {
      await restoreWarehouse(id)
      load(q, offset)
    } catch (e) {
      toast.error("恢复失败：" + String(e))
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const columns: ColumnDef<WarehouseItem>[] = [
    {
      id: "name",
      header: "名称",
      cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
    },
    {
      id: "code",
      header: "编号",
      cell: ({ row }) => <span className="text-muted-foreground">{row.original.code || "—"}</span>,
    },
    {
      id: "manager",
      header: "负责人",
      cell: ({ row }) => <span>{row.original.manager || "—"}</span>,
    },
    {
      id: "default",
      header: "默认",
      cell: ({ row }) => (row.original.isDefault ? <Badge tone="accent">默认</Badge> : null),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-3" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            onClick={() => openEdit(row.original)}
            className="text-xs text-primary hover:underline"
          >
            编辑
          </button>
          <button
            type="button"
            onClick={() => void handleDelete(row.original)}
            className="text-xs text-destructive hover:underline"
          >
            删除
          </button>
        </div>
      ),
    },
  ]

  const isForm = drawerMode === "create" || drawerMode === "edit"

  return (
    <PageContainer width="wide">
      <PageHeader
        title={<span data-testid="page-title">仓库管理</span>}
        subtitle={`共 ${total} 个仓库`}
        actions={<Button onClick={openCreate}>+ 新建仓库</Button>}
      />

      <div className="mb-4">
        <Input
          aria-label="搜索仓库"
          className="max-w-sm"
          placeholder="搜索仓库名称或编号..."
          value={q}
          onChange={(e) => handleSearchChange(e.target.value)}
        />
      </div>

      <DataTable
        columns={columns}
        data={items}
        loading={loading}
        error={error}
        getRowId={(item) => item.id}
        onRowClick={openDetail}
        animateRows
        empty={
          <EmptyState
            title="暂无仓库"
            description="添加仓库后可在进出货单中快速选择"
            action={<Button variant="outline" onClick={openCreate}>新建第一个仓库</Button>}
          />
        }
      />

      <Pagination
        page={currentPage}
        totalPages={totalPages}
        onPageChange={(p) => setOffset((p - 1) * PAGE_SIZE)}
      />

      <Sheet
        open={showDrawer}
        onOpenChange={(o) => {
          if (!o) closeDrawer()
        }}
        title={
          drawerMode === "create"
            ? "新建仓库"
            : drawerMode === "edit"
              ? "编辑仓库"
              : drawerItem?.name
        }
        footer={
          isForm ? (
            <>
              <Button variant="outline" size="sm" onClick={closeDrawer}>
                取消
              </Button>
              <Button size="sm" disabled={saving} onClick={() => void handleSave()}>
                {saving ? "保存中..." : "保存"}
              </Button>
            </>
          ) : drawerItem ? (
            <>
              <Button variant="outline" size="sm" onClick={() => void handleRestore(drawerItem.id)}>
                恢复
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  void handleDelete(drawerItem)
                  closeDrawer()
                }}
              >
                软删除
              </Button>
              <Button size="sm" onClick={() => openEdit(drawerItem)}>
                编辑
              </Button>
            </>
          ) : undefined
        }
      >
        {drawerMode === "view" && drawerItem ? (
          <div className="flex flex-col gap-3 text-sm" data-testid="warehouse-drawer">
            {[
              { label: "编号", value: drawerItem.code },
              { label: "负责人", value: drawerItem.manager },
              { label: "地址", value: drawerItem.address },
              { label: "备注", value: drawerItem.remark },
            ].map(({ label, value }) =>
              value ? (
                <div key={label}>
                  <span className="text-muted-foreground">{label}：</span>
                  <span>{value}</span>
                </div>
              ) : null
            )}
            {drawerItem.isDefault && <Badge tone="accent">默认仓库</Badge>}
          </div>
        ) : (
          <div className="flex flex-col gap-4" data-testid="warehouse-drawer">
            {TEXT_FIELDS.map(({ key, label, required, placeholder }) => (
              <div key={key} className="flex flex-col gap-1.5">
                <Label htmlFor={`warehouse-${key}`}>
                  {label}
                  {required && <span className="text-destructive"> *</span>}
                </Label>
                <Input
                  id={`warehouse-${key}`}
                  value={form[key]}
                  onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
                  placeholder={placeholder}
                />
              </div>
            ))}
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <Checkbox
                checked={formIsDefault}
                onCheckedChange={(checked) => setFormIsDefault(checked === true)}
              />
              设为默认仓库
            </label>
          </div>
        )}
      </Sheet>
    </PageContainer>
  )
}
