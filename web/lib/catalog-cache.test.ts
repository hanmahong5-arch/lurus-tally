import { describe, it, expect, vi, beforeEach } from "vitest"

vi.mock("idb-keyval", () => {
  const store = new Map<string, unknown>()
  return {
    createStore: vi.fn(() => "store"),
    get: vi.fn(async (k: string) => store.get(k)),
    set: vi.fn(async (k: string, v: unknown) => void store.set(k, v)),
    _store: store,
  }
})
vi.mock("@/lib/api/products", () => ({ listProducts: vi.fn() }))

import * as idb from "idb-keyval"
import { listProducts } from "@/lib/api/products"
import {
  CATALOG_PAGE_SIZE,
  catalogKey,
  refreshCatalog,
  searchCatalog,
  searchItems,
  _resetCatalogForTest,
  type CatalogItem,
} from "./catalog-cache"

const mem = (idb as unknown as { _store: Map<string, unknown> })._store

function item(i: number, over: Partial<CatalogItem> = {}): CatalogItem {
  return { id: `p${i}`, name: `商品${i}`, code: `C${i}`, barcode: `69000000${i}`, sale_price: "1.00", unit: "个", ...over }
}

describe("catalog-cache 搜索", () => {
  it("条码精确命中优先且只返回精确项", () => {
    const items = [item(1), item(2, { name: "690000001 套装" })]
    const r = searchItems(items, "690000001")
    expect(r.map((x) => x.id)).toEqual(["p1"])
  })

  it("名称与编码按包含匹配，不区分大小写", () => {
    const items = [item(1, { name: "Steel Bolt" }), item(2, { code: "BOLT-9" }), item(3)]
    expect(searchItems(items, "bolt").map((x) => x.id)).toEqual(["p1", "p2"])
  })

  it("结果最多 8 条", () => {
    const items = Array.from({ length: 30 }, (_, i) => item(i, { name: `螺丝${i}` }))
    expect(searchItems(items, "螺丝")).toHaveLength(8)
  })

  it("空查询不返回", () => {
    expect(searchItems([item(1)], "  ")).toEqual([])
  })
})

describe("catalog-cache 刷新", () => {
  beforeEach(() => {
    mem.clear()
    _resetCatalogForTest()
    vi.mocked(listProducts).mockReset()
  })

  it("分页拉完后写入缓存并可搜索", async () => {
    const mk = (n: number, start: number) =>
      Array.from({ length: n }, (_, i) => ({
        id: `p${start + i}`,
        name: `螺丝${start + i}`,
        code: `C${start + i}`,
        retail_price: "2.5",
        retail_price_unit: "个",
        attributes: { barcode: `B${start + i}` },
      }))
    vi.mocked(listProducts)
      .mockResolvedValueOnce({ items: mk(CATALOG_PAGE_SIZE, 0), total: CATALOG_PAGE_SIZE + 3 } as never)
      .mockResolvedValueOnce({ items: mk(3, CATALOG_PAGE_SIZE), total: CATALOG_PAGE_SIZE + 3 } as never)
    expect(await refreshCatalog("t1")).toBe(true)
    expect(listProducts).toHaveBeenCalledTimes(2)
    expect(mem.has(catalogKey("t1"))).toBe(true)
    const hit = await searchCatalog("t1", `B${CATALOG_PAGE_SIZE + 1}`)
    expect(hit[0].name).toBe(`螺丝${CATALOG_PAGE_SIZE + 1}`)
    expect(hit[0].sale_price).toBe("2.5")
  })

  it("拉取失败时保留旧缓存", async () => {
    mem.set(catalogKey("t1"), { updated_at: 1, items: [item(1)] })
    vi.mocked(listProducts).mockRejectedValue(new Error("x"))
    expect(await refreshCatalog("t1")).toBe(false)
    expect((mem.get(catalogKey("t1")) as { items: unknown[] }).items).toHaveLength(1)
  })
})
