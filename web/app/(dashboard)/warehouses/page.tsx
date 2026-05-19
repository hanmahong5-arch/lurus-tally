"use client"

import { useCallback, useRef, useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { toast } from "sonner"
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
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"

const PAGE_SIZE = 20

/**
 * WarehousesPage renders the warehouse list with search, pagination, and
 * a create/edit drawer (W3.D1).
 */
export default function WarehousesPage() {
  const [items, setItems] = useState<WarehouseItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [offset, setOffset] = useState(0)

  // Drawer state
  const [drawerItem, setDrawerItem] = useState<WarehouseItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">("view")
  const [showDrawer, setShowDrawer] = useState(false)
  const [saving, setSaving] = useState(false)

  // Form state
  const [formName, setFormName] = useState("")
  const [formCode, setFormCode] = useState("")
  const [formAddress, setFormAddress] = useState("")
  const [formManager, setFormManager] = useState("")
  const [formIsDefault, setFormIsDefault] = useState(false)
  const [formRemark, setFormRemark] = useState("")

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
    setFormName("")
    setFormCode("")
    setFormAddress("")
    setFormManager("")
    setFormIsDefault(false)
    setFormRemark("")
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
    setFormName(item.name)
    setFormCode(item.code)
    setFormAddress(item.address)
    setFormManager(item.manager)
    setFormIsDefault(item.isDefault)
    setFormRemark(item.remark)
    setShowDrawer(true)
  }

  function closeDrawer() {
    setShowDrawer(false)
    setDrawerItem(null)
  }

  async function handleSave() {
    if (!formName.trim()) {
      toast.error("仓库名称不能为空")
      return
    }
    setSaving(true)
    try {
      const input: WarehouseCreateInput = {
        name: formName.trim(),
        code: formCode.trim() || undefined,
        address: formAddress.trim() || undefined,
        manager: formManager.trim() || undefined,
        isDefault: formIsDefault,
        remark: formRemark.trim() || undefined,
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

  const totalPages = Math.ceil(total / PAGE_SIZE)
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  return (
    <div className="p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold" data-testid="page-title">
            仓库管理
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">共 {total} 个仓库</p>
        </div>
        <button
          onClick={openCreate}
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          + 新建仓库
        </button>
      </div>

      {/* Search */}
      <div className="mb-4">
        <input
          aria-label="搜索仓库"
          className="w-full max-w-sm rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          placeholder="搜索仓库名称或编号..."
          value={q}
          onChange={(e) => handleSearchChange(e.target.value)}
        />
      </div>

      {/* States */}
      {loading && <div className="py-12 text-center text-muted-foreground">加载中...</div>}
      {error && <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>}
      {!loading && !error && items.length === 0 && (
        <EmptyState
          title="暂无仓库"
          description="添加仓库后可在进出货单中快速选择"
          action={
            <button
              onClick={openCreate}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-sm hover:bg-muted transition-colors"
            >
              新建第一个仓库
            </button>
          }
        />
      )}

      {/* Table */}
      {!loading && items.length > 0 && (
        <div className="rounded-lg border border-border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground">名称</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground">编号</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground">负责人</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground">默认</th>
                <th className="text-right px-4 py-2.5 font-medium text-muted-foreground">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {items.map((item) => (
                <tr
                  key={item.id}
                  className="hover:bg-muted/30 transition-colors cursor-pointer"
                  onClick={() => openDetail(item)}
                >
                  <td className="px-4 py-3 font-medium">{item.name}</td>
                  <td className="px-4 py-3 text-muted-foreground">{item.code || "—"}</td>
                  <td className="px-4 py-3">{item.manager || "—"}</td>
                  <td className="px-4 py-3">
                    {item.isDefault ? (
                      <span className="rounded-full bg-primary/10 text-primary px-2 py-0.5 text-xs">
                        默认
                      </span>
                    ) : null}
                  </td>
                  <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                    <button
                      onClick={() => openEdit(item)}
                      className="text-xs text-primary hover:underline mr-3"
                    >
                      编辑
                    </button>
                    <button
                      onClick={() => void handleDelete(item)}
                      className="text-xs text-destructive hover:underline"
                    >
                      删除
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Pagination */}
      {total > PAGE_SIZE && (
        <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
          <span>
            第 {currentPage} / {totalPages} 页，共 {total} 条
          </span>
          <div className="flex gap-2">
            <button
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              className="rounded-md border border-border px-3 py-1 hover:bg-muted disabled:opacity-50"
            >
              上一页
            </button>
            <button
              disabled={offset + PAGE_SIZE >= total}
              onClick={() => setOffset(offset + PAGE_SIZE)}
              className="rounded-md border border-border px-3 py-1 hover:bg-muted disabled:opacity-50"
            >
              下一页
            </button>
          </div>
        </div>
      )}

      {/* Detail / Create / Edit drawer */}
      {showDrawer && (
        <div className="fixed inset-0 z-50 flex">
          <div className="flex-1 bg-black/30" onClick={closeDrawer} />
          <div
            className="w-[420px] bg-background border-l border-border overflow-y-auto p-6 flex flex-col gap-4"
            data-testid="warehouse-drawer"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold">
                {drawerMode === "create" ? "新建仓库" : drawerMode === "edit" ? "编辑仓库" : drawerItem?.name}
              </h2>
              <button onClick={closeDrawer} className="text-muted-foreground hover:text-foreground">
                ✕
              </button>
            </div>

            {drawerMode === "view" && drawerItem ? (
              <div className="flex flex-col gap-3 text-sm">
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
                {drawerItem.isDefault && (
                  <span className="rounded-full bg-primary/10 text-primary px-2 py-0.5 text-xs w-fit">
                    默认仓库
                  </span>
                )}
                <div className="flex gap-2 pt-2">
                  <button
                    onClick={() => openEdit(drawerItem)}
                    className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90"
                  >
                    编辑
                  </button>
                  <button
                    onClick={() => { void handleDelete(drawerItem); closeDrawer() }}
                    className="rounded-lg border border-destructive px-4 py-1.5 text-sm text-destructive hover:bg-destructive/10"
                  >
                    软删除
                  </button>
                  <button
                    onClick={() => void handleRestore(drawerItem.id)}
                    className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted"
                  >
                    恢复
                  </button>
                </div>
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">名称 *</label>
                  <input
                    className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
                    value={formName}
                    onChange={(e) => setFormName(e.target.value)}
                    placeholder="仓库名称"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">编号</label>
                  <input
                    className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
                    value={formCode}
                    onChange={(e) => setFormCode(e.target.value)}
                    placeholder="可选"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">负责人</label>
                  <input
                    className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
                    value={formManager}
                    onChange={(e) => setFormManager(e.target.value)}
                    placeholder="可选"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">地址</label>
                  <input
                    className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
                    value={formAddress}
                    onChange={(e) => setFormAddress(e.target.value)}
                    placeholder="可选"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">备注</label>
                  <input
                    className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
                    value={formRemark}
                    onChange={(e) => setFormRemark(e.target.value)}
                    placeholder="可选"
                  />
                </div>
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={formIsDefault}
                    onChange={(e) => setFormIsDefault(e.target.checked)}
                    className="rounded"
                  />
                  设为默认仓库
                </label>
                <div className="flex gap-2 pt-2">
                  <button
                    onClick={() => void handleSave()}
                    disabled={saving}
                    className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                  >
                    {saving ? "保存中..." : "保存"}
                  </button>
                  <button
                    onClick={closeDrawer}
                    className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted"
                  >
                    取消
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
