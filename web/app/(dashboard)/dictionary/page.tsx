"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import {
  listNurseryDict,
  deleteNurseryDict,
  restoreNurseryDict,
  type NurseryDictItem,
  type NurseryType,
} from "@/lib/api/nursery-dict"
import NurseryDictForm from "@/components/horticulture/NurseryDictForm"

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

const ALL_TYPES: NurseryType[] = [
  "tree",
  "shrub",
  "herb",
  "vine",
  "bamboo",
  "aquatic",
  "bulb",
  "fruit",
]

const PAGE_SIZE = 20

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

  // Drawer state: null = closed, 'create' = new form, item = detail/edit
  const [drawerItem, setDrawerItem] = useState<NurseryDictItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">("view")
  const [showDrawer, setShowDrawer] = useState(false)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(
    (query: string, type: NurseryType | "", off: number) => {
      setLoading(true)
      setError(null)
      listNurseryDict({
        q: query || undefined,
        type: type || undefined,
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
    load(q, typeFilter, offset)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [offset])

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
    if (!window.confirm(`确认软删除「${item.name}」?`)) return
    try {
      await deleteNurseryDict(item.id)
      load(q, typeFilter, offset)
    } catch (e) {
      alert("删除失败: " + String(e))
    }
  }

  async function handleRestoreById(id: string) {
    try {
      await restoreNurseryDict(id)
      load(q, typeFilter, offset)
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
          <h1 className="text-xl font-semibold">苗木字典</h1>
          <p className="text-sm text-muted-foreground mt-0.5" data-testid="total-count">
            共 {total} 种苗木
          </p>
        </div>
        <button
          onClick={openCreate}
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          + 新增苗木
        </button>
      </div>

      {/* Search + filter bar */}
      <div className="mb-4 flex gap-2">
        <input
          className="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          placeholder="搜索苗木名称..."
          value={q}
          onChange={(e) => handleSearchChange(e.target.value)}
        />
        <select
          className="rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none"
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
          暂无苗木，点击&quot;新增苗木&quot;添加第一个品种
        </div>
      )}

      {/* Table */}
      {!loading && items.length > 0 && (
        <div className="overflow-hidden rounded-xl border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 text-left font-medium">名称</th>
                <th className="px-4 py-2.5 text-left font-medium">拉丁名</th>
                <th className="px-4 py-2.5 text-left font-medium">科</th>
                <th className="px-4 py-2.5 text-left font-medium">类型</th>
                <th className="px-4 py-2.5 text-left font-medium">落叶/常绿</th>
                <th className="px-4 py-2.5 text-left font-medium">默认单位</th>
                <th className="px-4 py-2.5 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {items.map((item) => (
                <tr
                  key={item.id}
                  className="hover:bg-muted/30 transition-colors cursor-pointer"
                  onClick={() => openDetail(item)}
                >
                  <td className="px-4 py-2.5 font-medium">{item.name}</td>
                  <td className="px-4 py-2.5 text-muted-foreground italic text-xs">
                    {item.latin_name || "—"}
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground">
                    {item.family || "—"}
                  </td>
                  <td className="px-4 py-2.5">
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs">
                      {TYPE_LABELS[item.type] ?? item.type}
                    </span>
                  </td>
                  <td className="px-4 py-2.5">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs ${
                        item.is_evergreen
                          ? "bg-green-500/10 text-green-600"
                          : "bg-amber-500/10 text-amber-600"
                      }`}
                    >
                      {item.is_evergreen ? "常绿" : "落叶"}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground text-xs">
                    {item.default_unit_id ?? "—"}
                  </td>
                  <td
                    className="px-4 py-2.5 text-right"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <div className="flex justify-end gap-2">
                      <button
                        onClick={() => {
                          setDrawerItem(item)
                          setDrawerMode("edit")
                          setShowDrawer(true)
                        }}
                        className="text-xs text-primary hover:underline"
                      >
                        编辑
                      </button>
                      <button
                        onClick={() => handleDelete(item)}
                        className="text-xs text-destructive hover:underline"
                      >
                        删除
                      </button>
                    </div>
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
          <div
            className="flex-1 bg-black/30"
            onClick={closeDrawer}
          />
          {/* Sheet panel */}
          <div
            className="w-[480px] bg-background border-l border-border overflow-y-auto p-6 flex flex-col gap-4"
            data-testid="nursery-detail-drawer"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold">
                {drawerMode === "create"
                  ? "新增苗木"
                  : drawerMode === "edit"
                  ? "编辑苗木"
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
                  <div className="mt-1 rounded-md bg-muted p-2 text-xs font-mono">
                    {Object.keys(drawerItem.spec_template).length > 0
                      ? Object.keys(drawerItem.spec_template).map((k) => (
                          <div key={k}>{k}</div>
                        ))
                      : "—"}
                  </div>
                </div>
                <div>
                  <span className="text-muted-foreground">备注：</span>
                  <span>{drawerItem.remark || "—"}</span>
                </div>
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
              <NurseryDictForm
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
