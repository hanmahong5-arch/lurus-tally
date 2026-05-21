"use client"

import { useCallback, useRef, useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  listSuppliers,
  deleteSupplier,
  restoreSupplier,
  createSupplier,
  updateSupplier,
  type SupplierItem,
  type SupplierCreateInput,
} from "@/lib/api/suppliers"
import { useConfirm } from "@/hooks/useConfirm"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable } from "@/components/ui/data-table"
import { Pagination } from "@/components/ui/pagination"
import { Sheet } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { EmptyState } from "@/components/ui/empty-state"

const PAGE_SIZE = 20

interface SupplierForm {
  name: string
  code: string
  contact: string
  phone: string
  email: string
  address: string
  remark: string
}

const FORM_FIELDS: { key: keyof SupplierForm; label: string; required?: boolean; placeholder: string }[] = [
  { key: "name", label: "名称", required: true, placeholder: "供应商名称" },
  { key: "code", label: "编号", placeholder: "可选" },
  { key: "contact", label: "联系人", placeholder: "可选" },
  { key: "phone", label: "电话", placeholder: "可选" },
  { key: "email", label: "邮箱", placeholder: "可选" },
  { key: "address", label: "地址", placeholder: "可选" },
  { key: "remark", label: "备注", placeholder: "可选" },
]

const EMPTY_FORM: SupplierForm = {
  name: "",
  code: "",
  contact: "",
  phone: "",
  email: "",
  address: "",
  remark: "",
}

/**
 * SuppliersPage renders the supplier list with search, pagination, and
 * a create/edit/view drawer (W3.D1).
 */
export default function SuppliersPage() {
  const [items, setItems] = useState<SupplierItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [offset, setOffset] = useState(0)

  const [drawerItem, setDrawerItem] = useState<SupplierItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">("view")
  const [showDrawer, setShowDrawer] = useState(false)
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState<SupplierForm>(EMPTY_FORM)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const confirm = useConfirm()

  const load = useCallback(
    (query: string, off: number, signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listSuppliers({ q: query || undefined, limit: PAGE_SIZE, offset: off, signal, retry: 2 })
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
    setShowDrawer(true)
  }

  function openDetail(item: SupplierItem) {
    setDrawerItem(item)
    setDrawerMode("view")
    setShowDrawer(true)
  }

  function openEdit(item: SupplierItem) {
    setDrawerItem(item)
    setDrawerMode("edit")
    setForm({
      name: item.name,
      code: item.code,
      contact: item.contact,
      phone: item.phone,
      email: item.email,
      address: item.address,
      remark: item.remark,
    })
    setShowDrawer(true)
  }

  function closeDrawer() {
    setShowDrawer(false)
    setDrawerItem(null)
  }

  async function handleSave() {
    if (!form.name.trim()) {
      toast.error("供应商名称不能为空")
      return
    }
    setSaving(true)
    try {
      const input: SupplierCreateInput = {
        name: form.name.trim(),
        code: form.code.trim() || undefined,
        contact: form.contact.trim() || undefined,
        phone: form.phone.trim() || undefined,
        email: form.email.trim() || undefined,
        address: form.address.trim() || undefined,
        remark: form.remark.trim() || undefined,
      }
      if (drawerMode === "create") {
        await createSupplier(input)
        toast.success("供应商已创建")
      } else if (drawerItem) {
        await updateSupplier(drawerItem.id, input)
        toast.success("供应商已更新")
      }
      closeDrawer()
      load(q, offset)
    } catch (e) {
      toast.error("保存失败：" + String(e))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(item: SupplierItem) {
    const ok = await confirm({
      title: "软删除供应商",
      body: `确认软删除「${item.name}」？删除后可在列表中恢复。`,
      confirmText: "删除",
      danger: true,
    })
    if (!ok) return
    try {
      await deleteSupplier(item.id)
      load(q, offset)
    } catch (e) {
      toast.error("删除失败：" + String(e))
    }
  }

  async function handleRestore(id: string) {
    try {
      await restoreSupplier(id)
      load(q, offset)
    } catch (e) {
      toast.error("恢复失败：" + String(e))
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const columns: ColumnDef<SupplierItem>[] = [
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
      id: "contact",
      header: "联系人",
      cell: ({ row }) => <span>{row.original.contact || "—"}</span>,
    },
    {
      id: "phone",
      header: "电话",
      cell: ({ row }) => <span>{row.original.phone || "—"}</span>,
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
        title={<span data-testid="page-title">供应商管理</span>}
        subtitle={`共 ${total} 个供应商`}
        actions={<Button onClick={openCreate}>+ 新增供应商</Button>}
      />

      <div className="mb-4">
        <Input
          aria-label="搜索供应商"
          className="max-w-sm"
          placeholder="搜索供应商名称或编号..."
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
            title="暂无供应商"
            description="添加供应商后可在采购单中快速引用"
            action={<Button variant="outline" onClick={openCreate}>新增第一个供应商</Button>}
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
            ? "新增供应商"
            : drawerMode === "edit"
              ? "编辑供应商"
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
          <div className="flex flex-col gap-3 text-sm" data-testid="supplier-drawer">
            {[
              { label: "编号", value: drawerItem.code },
              { label: "联系人", value: drawerItem.contact },
              { label: "电话", value: drawerItem.phone },
              { label: "邮箱", value: drawerItem.email },
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
          </div>
        ) : (
          <div className="flex flex-col gap-4" data-testid="supplier-drawer">
            {FORM_FIELDS.map(({ key, label, required, placeholder }) => (
              <div key={key} className="flex flex-col gap-1.5">
                <Label htmlFor={`supplier-${key}`}>
                  {label}
                  {required && <span className="text-destructive"> *</span>}
                </Label>
                <Input
                  id={`supplier-${key}`}
                  value={form[key]}
                  onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
                  placeholder={placeholder}
                />
              </div>
            ))}
          </div>
        )}
      </Sheet>
    </PageContainer>
  )
}
