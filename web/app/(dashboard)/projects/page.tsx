"use client"

import { useCallback, useRef, useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { toast } from "sonner"
import {
  listProjects,
  deleteProject,
  restoreProject,
  type ProjectItem,
  type ProjectStatus,
} from "@/lib/api/projects"
import ProjectForm from "@/components/project/ProjectForm"
import { useConfirm } from "@/hooks/useConfirm"
import { formatCNY } from "@/lib/format"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Pagination } from "@/components/ui/pagination"
import { Sheet } from "@/components/ui/sheet"
import { Badge, type BadgeTone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EmptyState } from "@/components/ui/empty-state"
import { Skeleton } from "@/components/ui/skeleton"

const STATUS_OPTIONS: { value: ProjectStatus | ""; label: string }[] = [
  { value: "", label: "全部" },
  { value: "active", label: "进行中" },
  { value: "paused", label: "已暂停" },
  { value: "completed", label: "已完工" },
  { value: "cancelled", label: "已取消" },
]

const STATUS_TONE: Record<ProjectStatus, BadgeTone> = {
  active: "ok",
  paused: "warn",
  completed: "neutral",
  cancelled: "err",
}

const STATUS_LABEL: Record<ProjectStatus, string> = {
  active: "进行中",
  paused: "已暂停",
  completed: "已完工",
  cancelled: "已取消",
}

const SELECT_CLASS =
  "h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

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

  const [drawerItem, setDrawerItem] = useState<ProjectItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">("view")
  const [showDrawer, setShowDrawer] = useState(false)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const confirm = useConfirm()

  const load = useCallback(
    (query: string, status: ProjectStatus | "", off: number, signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listProjects({
        q: query || undefined,
        status: status || undefined,
        limit: PAGE_SIZE,
        offset: off,
        signal,
        retry: 2,
      })
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
    load(q, statusFilter, offset, signal, isCancelled)
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
    const ok = await confirm({
      title: "软删除项目",
      body: `确认软删除「${item.name}」？删除后可在筛选中恢复。`,
      confirmText: "删除",
      danger: true,
    })
    if (!ok) return
    try {
      await deleteProject(item.id)
      load(q, statusFilter, offset)
    } catch (e) {
      toast.error("删除失败：" + String(e))
    }
  }

  async function handleRestoreById(id: string) {
    try {
      await restoreProject(id)
      load(q, statusFilter, offset)
    } catch (e) {
      toast.error("恢复失败：" + String(e))
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  return (
    <PageContainer width="wide">
      <PageHeader
        title={<span data-testid="page-title">项目</span>}
        subtitle={`共 ${total} 个项目`}
        actions={<Button onClick={openCreate}>+ 新建项目</Button>}
      />

      <div className="mb-4 flex gap-2">
        <Input
          aria-label="搜索项目"
          className="flex-1"
          placeholder="搜索项目名称或编号..."
          value={q}
          onChange={(e) => handleSearchChange(e.target.value)}
        />
        <select
          aria-label="按状态筛选"
          className={SELECT_CLASS}
          value={statusFilter}
          onChange={(e) => handleStatusChange(e.target.value as ProjectStatus | "")}
        >
          {STATUS_OPTIONS.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </div>

      {loading && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-32" />
          ))}
        </div>
      )}
      {error && <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>}
      {!loading && !error && items.length === 0 && (
        <EmptyState
          title="暂无项目"
          description="按项目跟踪进度和成本，把苗木和工序串起来"
          action={<Button variant="outline" onClick={openCreate}>新建第一个项目</Button>}
        />
      )}

      {!loading && items.length > 0 && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((item) => (
            <div
              key={item.id}
              data-testid="project-card"
              role="button"
              tabIndex={0}
              className="cursor-pointer rounded-xl border border-border bg-card p-4 shadow-sm transition-shadow hover:shadow-md focus:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
              onClick={() => openDetail(item)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault()
                  openDetail(item)
                }
              }}
            >
              <h3 className="truncate font-semibold text-foreground">{item.name}</h3>
              <p className="mb-2 text-sm text-muted-foreground">{item.code}</p>

              {item.customerId && (
                <span className="mr-2 rounded-full bg-muted px-2 py-0.5 text-xs">{item.customerId}</span>
              )}

              {item.contractAmount && (
                <p className="mt-1 text-lg font-bold tabular-nums text-foreground">
                  {formatCNY(item.contractAmount)}
                </p>
              )}

              <div className="mt-2">
                <Badge tone={STATUS_TONE[item.status] ?? "neutral"}>
                  {STATUS_LABEL[item.status] ?? item.status}
                </Badge>
              </div>

              <p className="mt-2 text-xs text-muted-foreground">
                {item.startDate ?? "—"} – {item.endDate ?? "—"}
              </p>
            </div>
          ))}
        </div>
      )}

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
          drawerMode === "create" ? "新建项目" : drawerMode === "edit" ? "编辑项目" : drawerItem?.name
        }
        footer={
          drawerMode === "view" && drawerItem ? (
            <>
              <Button variant="outline" size="sm" onClick={() => void handleRestoreById(drawerItem.id)}>
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
              <Button size="sm" onClick={() => setDrawerMode("edit")}>
                编辑
              </Button>
            </>
          ) : undefined
        }
      >
        {drawerMode === "view" && drawerItem ? (
          <div className="flex flex-col gap-3 text-sm" data-testid="project-detail-drawer">
            <div>
              <span className="text-muted-foreground">编号：</span>
              <span>{drawerItem.code}</span>
            </div>
            <div>
              <span className="text-muted-foreground">状态：</span>
              <Badge tone={STATUS_TONE[drawerItem.status] ?? "neutral"}>
                {STATUS_LABEL[drawerItem.status] ?? drawerItem.status}
              </Badge>
            </div>
            {drawerItem.contractAmount && (
              <div>
                <span className="text-muted-foreground">合同金额：</span>
                <span className="tabular-nums">{formatCNY(drawerItem.contractAmount)}</span>
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
                {drawerItem.startDate ?? "—"} – {drawerItem.endDate ?? "—"}
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
          </div>
        ) : (
          <ProjectForm
            mode={drawerMode === "create" ? "create" : "edit"}
            initialData={drawerItem ?? undefined}
            onSuccess={handleFormSuccess}
            onCancel={closeDrawer}
          />
        )}
      </Sheet>
    </PageContainer>
  )
}
