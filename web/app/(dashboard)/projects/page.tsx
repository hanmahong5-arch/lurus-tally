"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import {
  listProjects,
  deleteProject,
  restoreProject,
  type ProjectItem,
  type ProjectStatus,
} from "@/lib/api/projects"
import ProjectForm from "@/components/project/ProjectForm"

const STATUS_OPTIONS: { value: ProjectStatus | ""; label: string }[] = [
  { value: "", label: "全部" },
  { value: "active", label: "进行中" },
  { value: "paused", label: "已暂停" },
  { value: "completed", label: "已完工" },
  { value: "cancelled", label: "已取消" },
]

function statusBadgeClass(status: ProjectStatus): string {
  switch (status) {
    case "active":
      return "bg-green-500/10 text-green-700 dark:text-green-400"
    case "paused":
      return "bg-yellow-500/10 text-yellow-700 dark:text-yellow-400"
    case "completed":
      return "bg-gray-500/10 text-gray-600 dark:text-gray-400"
    case "cancelled":
      return "bg-red-500/10 text-red-600 dark:text-red-400"
    default:
      return "bg-muted text-muted-foreground"
  }
}

function statusLabel(status: ProjectStatus): string {
  switch (status) {
    case "active":
      return "进行中"
    case "paused":
      return "已暂停"
    case "completed":
      return "已完工"
    case "cancelled":
      return "已取消"
    default:
      return status
  }
}

const PAGE_SIZE = 20

/**
 * ProjectsPage renders the projects list as a card grid with search,
 * status filter, pagination, and a detail/create drawer (Story 28.2).
 */
export default function ProjectsPage() {
  const [items, setItems] = useState<ProjectItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [statusFilter, setStatusFilter] = useState<ProjectStatus | "">("")
  const [offset, setOffset] = useState(0)

  // Drawer state
  const [drawerItem, setDrawerItem] = useState<ProjectItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">(
    "view"
  )
  const [showDrawer, setShowDrawer] = useState(false)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(
    (query: string, status: ProjectStatus | "", off: number) => {
      setLoading(true)
      setError(null)
      listProjects({
        q: query || undefined,
        status: status || undefined,
        limit: PAGE_SIZE,
        offset: off,
      })
        .then((res) => {
          setItems(res.items ?? [])
          setTotal(res.total)
        })
        .catch((e) => setError(String(e)))
        .finally(() => setLoading(false))
    },
    []
  )

  useEffect(() => {
    load(q, statusFilter, offset)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [offset])

  function handleSearchChange(value: string) {
    setQ(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      setOffset(0)
      load(value, statusFilter, 0)
    }, 300)
  }

  function handleStatusChange(value: ProjectStatus | "") {
    setStatusFilter(value)
    setOffset(0)
    load(q, value, 0)
  }

  function openCreate() {
    setDrawerItem(null)
    setDrawerMode("create")
    setShowDrawer(true)
  }

  function openDetail(item: ProjectItem) {
    setDrawerItem(item)
    setDrawerMode("view")
    setShowDrawer(true)
  }

  function closeDrawer() {
    setShowDrawer(false)
    setDrawerItem(null)
  }

  function handleFormSuccess() {
    closeDrawer()
    load(q, statusFilter, offset)
  }

  async function handleDelete(item: ProjectItem) {
    if (!window.confirm(`确认软删除「${item.name}」?`)) return
    try {
      await deleteProject(item.id)
      load(q, statusFilter, offset)
    } catch (e) {
      alert("删除失败: " + String(e))
    }
  }

  async function handleRestoreById(id: string) {
    try {
      await restoreProject(id)
      load(q, statusFilter, offset)
    } catch (e) {
      alert("恢复失败: " + String(e))
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
            项目
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            共 {total} 个项目
          </p>
        </div>
        <button
          onClick={openCreate}
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          + 新建项目
        </button>
      </div>

      {/* Search + filter bar */}
      <div className="mb-4 flex gap-2">
        <input
          className="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          placeholder="搜索项目名称或编号..."
          value={q}
          onChange={(e) => handleSearchChange(e.target.value)}
        />
        <select
          className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none"
          value={statusFilter}
          onChange={(e) =>
            handleStatusChange(e.target.value as ProjectStatus | "")
          }
        >
          {STATUS_OPTIONS.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </div>

      {/* States */}
      {loading && (
        <div className="py-12 text-center text-muted-foreground">加载中...</div>
      )}
      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}
      {!loading && !error && items.length === 0 && (
        <div className="py-12 text-center text-muted-foreground">
          暂无项目，点击&quot;新建项目&quot;添加第一个
        </div>
      )}

      {/* Card grid */}
      {!loading && items.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {items.map((item) => (
            <div
              key={item.id}
              data-testid="project-card"
              className="rounded-xl border border-border bg-background p-4 shadow-sm hover:shadow-md transition-shadow cursor-pointer"
              onClick={() => openDetail(item)}
            >
              {/* Name + Code */}
              <h3 className="font-semibold text-foreground truncate">
                {item.name}
              </h3>
              <p className="text-sm text-muted-foreground mb-2">{item.code}</p>

              {/* Customer badge */}
              {/* TODO(S28.8): resolve customer name from partner */}
              {item.customerId && (
                <span className="rounded-full bg-muted px-2 py-0.5 text-xs mr-2">
                  {item.customerId}
                </span>
              )}

              {/* Contract amount */}
              {item.contractAmount && (
                <p className="text-lg font-bold text-foreground mt-1">
                  ¥{item.contractAmount}
                </p>
              )}

              {/* Status badge */}
              <span
                className={`inline-block rounded-full px-2 py-0.5 text-xs mt-2 ${statusBadgeClass(item.status)}`}
              >
                {statusLabel(item.status)}
              </span>

              {/* Date range */}
              <p className="text-xs text-muted-foreground mt-2">
                {item.startDate ?? "—"} – {item.endDate ?? "—"}
              </p>
            </div>
          ))}
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
              className="rounded-md border border-border px-3 py-1 hover:bg-muted disabled:opacity-40"
            >
              上一页
            </button>
            <button
              disabled={offset + PAGE_SIZE >= total}
              onClick={() => setOffset(offset + PAGE_SIZE)}
              className="rounded-md border border-border px-3 py-1 hover:bg-muted disabled:opacity-40"
            >
              下一页
            </button>
          </div>
        </div>
      )}

      {/* Detail / Create / Edit drawer */}
      {showDrawer && (
        <div className="fixed inset-0 z-50 flex">
          {/* Backdrop */}
          <div className="flex-1 bg-black/30" onClick={closeDrawer} />
          {/* Sheet panel */}
          <div
            className="w-[480px] bg-background border-l border-border overflow-y-auto p-6 flex flex-col gap-4"
            data-testid="project-detail-drawer"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold">
                {drawerMode === "create"
                  ? "新建项目"
                  : drawerMode === "edit"
                    ? "编辑项目"
                    : drawerItem?.name}
              </h2>
              <button
                onClick={closeDrawer}
                className="text-muted-foreground hover:text-foreground"
              >
                ✕
              </button>
            </div>

            {drawerMode === "view" && drawerItem ? (
              <div className="flex flex-col gap-3 text-sm">
                <div>
                  <span className="text-muted-foreground">编号：</span>
                  <span>{drawerItem.code}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">状态：</span>
                  <span>{statusLabel(drawerItem.status)}</span>
                </div>
                {drawerItem.contractAmount && (
                  <div>
                    <span className="text-muted-foreground">合同金额：</span>
                    <span>¥{drawerItem.contractAmount}</span>
                  </div>
                )}
                {drawerItem.customerId && (
                  <div>
                    <span className="text-muted-foreground">客户ID：</span>
                    <span>{drawerItem.customerId}</span>
                  </div>
                )}
                <div>
                  <span className="text-muted-foreground">日期：</span>
                  <span>
                    {drawerItem.startDate ?? "—"} –{" "}
                    {drawerItem.endDate ?? "—"}
                  </span>
                </div>
                {drawerItem.address && (
                  <div>
                    <span className="text-muted-foreground">地址：</span>
                    <span>{drawerItem.address}</span>
                  </div>
                )}
                {drawerItem.manager && (
                  <div>
                    <span className="text-muted-foreground">负责人：</span>
                    <span>{drawerItem.manager}</span>
                  </div>
                )}
                {drawerItem.remark && (
                  <div>
                    <span className="text-muted-foreground">备注：</span>
                    <span>{drawerItem.remark}</span>
                  </div>
                )}
                <div className="flex gap-2 pt-2">
                  <button
                    onClick={() => setDrawerMode("edit")}
                    className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90"
                  >
                    编辑
                  </button>
                  <button
                    onClick={() => {
                      void handleDelete(drawerItem)
                      closeDrawer()
                    }}
                    className="rounded-lg border border-destructive px-4 py-1.5 text-sm text-destructive hover:bg-destructive/10"
                  >
                    软删除
                  </button>
                  <button
                    onClick={() => void handleRestoreById(drawerItem.id)}
                    className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted"
                  >
                    恢复
                  </button>
                </div>
              </div>
            ) : (
              <ProjectForm
                mode={drawerMode === "create" ? "create" : "edit"}
                initialData={drawerItem ?? undefined}
                onSuccess={handleFormSuccess}
                onCancel={closeDrawer}
              />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
