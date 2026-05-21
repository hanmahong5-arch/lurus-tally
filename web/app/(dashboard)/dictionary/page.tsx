"use client"

import { useCallback, useRef, useState } from "react"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  listNurseryDict,
  deleteNurseryDict,
  restoreNurseryDict,
  type NurseryDictItem,
  type NurseryType,
} from "@/lib/api/nursery-dict"
import NurseryDictForm from "@/components/horticulture/NurseryDictForm"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useConfirm } from "@/hooks/useConfirm"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable } from "@/components/ui/data-table"
import { Pagination } from "@/components/ui/pagination"
import { Sheet } from "@/components/ui/sheet"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EmptyState } from "@/components/ui/empty-state"

const TYPE_LABELS: Record<NurseryType, string> = {
  tree: "乔木",
  shrub: "灌木",
  herb: "地被",
  vine: "藤本",
  bamboo: "竹类",
  aquatic: "水生",
  bulb: "球根",
  fruit: "果树",
}

const ALL_TYPES: NurseryType[] = ["tree", "shrub", "herb", "vine", "bamboo", "aquatic", "bulb", "fruit"]

const PAGE_SIZE = 20

const SELECT_CLASS =
  "h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

/**
 * DictionaryPage renders the nursery species dictionary list with search,
 * type filter, pagination, and a detail/create drawer (Story 28.1).
 */
export default function DictionaryPage() {
  const [items, setItems] = useState<NurseryDictItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [typeFilter, setTypeFilter] = useState<NurseryType | "">("")
  const [offset, setOffset] = useState(0)

  const [drawerItem, setDrawerItem] = useState<NurseryDictItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">("view")
  const [showDrawer, setShowDrawer] = useState(false)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const confirm = useConfirm()

  const load = useCallback(
    (
      query: string,
      type: NurseryType | "",
      off: number,
      signal?: AbortSignal,
      isCancelled?: () => boolean,
    ) => {
      setLoading(true)
      setError(null)
      listNurseryDict({
        q: query || undefined,
        type: type || undefined,
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

  useAbortableEffect(
    (signal, isCancelled) => {
      load(q, typeFilter, offset, signal, isCancelled)
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [offset],
  )

  function handleSearchChange(value: string) {
    setQ(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      setOffset(0)
      load(value, typeFilter, 0)
    }, 300)
  }

  function handleTypeChange(value: NurseryType | "") {
    setTypeFilter(value)
    setOffset(0)
    load(q, value, 0)
  }

  function openCreate() {
    setDrawerItem(null)
    setDrawerMode("create")
    setShowDrawer(true)
  }

  function openDetail(item: NurseryDictItem) {
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
    load(q, typeFilter, offset)
  }

  async function handleDelete(item: NurseryDictItem) {
    const ok = await confirm({
      title: "软删除苗木",
      body: `确认软删除「${item.name}」？删除后可在筛选中恢复。`,
      confirmText: "删除",
      danger: true,
    })
    if (!ok) return
    try {
      await deleteNurseryDict(item.id)
      load(q, typeFilter, offset)
    } catch (e) {
      toast.error("删除失败：" + String(e))
    }
  }

  async function handleRestoreById(id: string) {
    try {
      await restoreNurseryDict(id)
      load(q, typeFilter, offset)
    } catch (e) {
      toast.error("恢复失败：" + String(e))
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const columns: ColumnDef<NurseryDictItem>[] = [
    {
      id: "name",
      header: "名称",
      cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
    },
    {
      id: "latin",
      header: "拉丁名",
      cell: ({ row }) => (
        <span className="text-xs italic text-muted-foreground">{row.original.latin_name || "—"}</span>
      ),
    },
    {
      id: "family",
      header: "科",
      cell: ({ row }) => <span className="text-muted-foreground">{row.original.family || "—"}</span>,
    },
    {
      id: "type",
      header: "类型",
      cell: ({ row }) => <Badge tone="neutral">{TYPE_LABELS[row.original.type] ?? row.original.type}</Badge>,
    },
    {
      id: "evergreen",
      header: "落叶/常绿",
      cell: ({ row }) => (
        <Badge tone={row.original.is_evergreen ? "ok" : "warn"}>
          {row.original.is_evergreen ? "常绿" : "落叶"}
        </Badge>
      ),
    },
    {
      id: "unit",
      header: "默认单位",
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">{row.original.default_unit_id ?? "—"}</span>
      ),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => (
        <div className="flex justify-end gap-3" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            onClick={() => {
              setDrawerItem(row.original)
              setDrawerMode("edit")
              setShowDrawer(true)
            }}
            className="text-xs text-primary hover:underline"
          >
            编辑
          </button>
          <button
            type="button"
            onClick={() => handleDelete(row.original)}
            className="text-xs text-destructive hover:underline"
          >
            删除
          </button>
        </div>
      ),
    },
  ]

  return (
    <PageContainer width="wide">
      <PageHeader
        title="苗木字典"
        subtitle={<span data-testid="total-count">共 {total} 种苗木</span>}
        actions={<Button onClick={openCreate}>+ 新增苗木</Button>}
      />

      <div className="mb-4 flex gap-2">
        <Input
          aria-label="搜索苗木"
          className="flex-1"
          placeholder="搜索苗木名称..."
          value={q}
          onChange={(e) => handleSearchChange(e.target.value)}
        />
        <select
          aria-label="按类型筛选"
          className={SELECT_CLASS}
          value={typeFilter}
          onChange={(e) => handleTypeChange(e.target.value as NurseryType | "")}
        >
          <option value="">全部类型</option>
          {ALL_TYPES.map((t) => (
            <option key={t} value={t}>
              {TYPE_LABELS[t]}
            </option>
          ))}
        </select>
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
            title="暂无苗木"
            description="开始建立你的苗木字典，方便后续采购和销售选品"
            action={<Button variant="outline" onClick={openCreate}>新增第一个苗木</Button>}
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
          drawerMode === "create" ? "新增苗木" : drawerMode === "edit" ? "编辑苗木" : drawerItem?.name
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
          <div className="flex flex-col gap-3 text-sm" data-testid="nursery-detail-drawer">
            <div>
              <span className="text-muted-foreground">拉丁名：</span>
              <span data-testid="latin-name">{drawerItem.latin_name || "—"}</span>
            </div>
            <div>
              <span className="text-muted-foreground">科：</span>
              <span>{drawerItem.family || "—"}</span>
            </div>
            <div>
              <span className="text-muted-foreground">属：</span>
              <span>{drawerItem.genus || "—"}</span>
            </div>
            <div>
              <span className="text-muted-foreground">类型：</span>
              <span>{TYPE_LABELS[drawerItem.type] ?? drawerItem.type}</span>
            </div>
            <div>
              <span className="text-muted-foreground">常绿：</span>
              <span>{drawerItem.is_evergreen ? "是" : "否"}</span>
            </div>
            <div>
              <span className="text-muted-foreground">气候带：</span>
              <span>{drawerItem.climate_zones.join("、") || "—"}</span>
            </div>
            <div>
              <span className="text-muted-foreground">最佳移植期：</span>
              <span>
                {drawerItem.best_season[0]
                  ? `${drawerItem.best_season[0]}月—${drawerItem.best_season[1]}月`
                  : "—"}
              </span>
            </div>
            <div>
              <span className="text-muted-foreground">规格模板：</span>
              <div className="mt-1 rounded-md bg-muted p-2 font-mono text-xs">
                {Object.keys(drawerItem.spec_template).length > 0
                  ? Object.keys(drawerItem.spec_template).map((k) => <div key={k}>{k}</div>)
                  : "—"}
              </div>
            </div>
            <div>
              <span className="text-muted-foreground">备注：</span>
              <span>{drawerItem.remark || "—"}</span>
            </div>
          </div>
        ) : (
          <NurseryDictForm
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
