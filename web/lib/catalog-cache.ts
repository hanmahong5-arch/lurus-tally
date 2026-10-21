/**
 * 收银目录缓存：把商品目录（名称、编码、条码、零售价、单位）放进 IndexedDB，
 * 断网时收银台仍能搜到商品、按「缓存价」加购。服务器永远是真源：
 * 缓存只在联网时整体覆盖，不做合并；库存数量不作为扣减依据。
 */
import { createStore, get, set, type UseStore } from "idb-keyval"
import { listProducts, type MeasurementStrategy, type Product } from "@/lib/api/products"

/** 一条缓存商品。last_on_hand 仅供展示参考，绝不用于扣减。 */
export interface CatalogItem {
  id: string
  name: string
  code: string
  barcode: string
  sale_price: string
  unit: string
  last_on_hand?: string
  /** 以下两项让离线加购能还原默认单位与计量方式。 */
  unit_id?: string
  measurement_strategy?: MeasurementStrategy
}

export interface CatalogSnapshot {
  updated_at: number
  items: CatalogItem[]
}

/** 单租户最多缓存的商品数。 */
export const CATALOG_MAX_ITEMS = 5000
/** 单页拉取条数（接口上限 500）。 */
export const CATALOG_PAGE_SIZE = 500
/** 搜索结果上限。 */
export const CATALOG_SEARCH_LIMIT = 8

let store: UseStore | null = null
function getStore(): UseStore {
  if (!store) store = createStore("tally-catalog", "kv")
  return store
}

export function catalogKey(tenantId: string): string {
  return `catalog@${tenantId}`
}

/** 商品转缓存条目（只留收银需要的字段）。 */
export function toCatalogItem(p: Product): CatalogItem {
  const barcode = p.attributes && typeof p.attributes.barcode === "string" ? p.attributes.barcode : ""
  return {
    id: p.id,
    name: p.name,
    code: p.code ?? "",
    barcode,
    sale_price: p.retail_price ?? "",
    unit: p.retail_price_unit ?? "",
    unit_id: p.default_unit_id,
    measurement_strategy: p.measurement_strategy,
  }
}

/** 缓存条目还原成收银台用的最小商品对象。 */
export function catalogItemToProduct(it: CatalogItem): Product {
  return {
    id: it.id,
    tenant_id: "",
    code: it.code,
    name: it.name,
    enabled: true,
    enable_serial_no: false,
    enable_lot_no: false,
    measurement_strategy: it.measurement_strategy ?? "individual",
    default_unit_id: it.unit_id,
    retail_price: it.sale_price || undefined,
    retail_price_unit: it.unit || undefined,
    attributes: it.barcode ? { barcode: it.barcode } : {},
    created_at: "",
    updated_at: "",
  }
}

/** 纯函数：条码精确命中优先，其次名称/编码包含；最多 8 条。 */
export function searchItems(items: CatalogItem[], q: string): CatalogItem[] {
  const needle = q.trim().toLowerCase()
  if (!needle) return []
  const exact = items.filter((it) => it.barcode !== "" && it.barcode.toLowerCase() === needle)
  if (exact.length > 0) return exact.slice(0, CATALOG_SEARCH_LIMIT)
  const out: CatalogItem[] = []
  for (const it of items) {
    if (it.name.toLowerCase().includes(needle) || it.code.toLowerCase().includes(needle)) {
      out.push(it)
      if (out.length >= CATALOG_SEARCH_LIMIT) break
    }
  }
  return out
}

export async function loadCatalog(tenantId: string): Promise<CatalogSnapshot | null> {
  if (typeof window === "undefined") return null
  try {
    const snap = await get<CatalogSnapshot>(catalogKey(tenantId), getStore())
    if (!snap || !Array.isArray(snap.items)) return null
    return snap
  } catch {
    return null
  }
}

/** 搜索缓存，同时返回缓存时间，供界面标「缓存价（x 分钟前）」。 */
export async function searchCatalogWithMeta(
  tenantId: string,
  q: string,
): Promise<{ items: CatalogItem[]; updated_at: number } | null> {
  const snap = await loadCatalog(tenantId)
  if (!snap) return null
  return { items: searchItems(snap.items, q), updated_at: snap.updated_at }
}

export async function searchCatalog(tenantId: string, q: string): Promise<CatalogItem[]> {
  return (await searchCatalogWithMeta(tenantId, q))?.items ?? []
}

function whenIdle(fn: () => void): void {
  const w = typeof window !== "undefined" ? (window as unknown as { requestIdleCallback?: (cb: () => void) => number }) : undefined
  if (w?.requestIdleCallback) w.requestIdleCallback(fn)
  else setTimeout(fn, 0)
}

let inflight: Promise<boolean> | null = null

/**
 * 后台分页拉取全量目录（空闲时执行），拉完或到 5000 条上限后整体覆盖缓存。
 * 中途失败则保留旧缓存。返回是否刷新成功。
 */
export function refreshCatalog(tenantId: string | undefined): Promise<boolean> {
  if (!tenantId || typeof window === "undefined") return Promise.resolve(false)
  if (typeof navigator !== "undefined" && navigator.onLine === false) return Promise.resolve(false)
  if (inflight) return inflight
  inflight = new Promise<boolean>((resolve) => {
    whenIdle(async () => {
      try {
        const items: CatalogItem[] = []
        let offset = 0
        while (items.length < CATALOG_MAX_ITEMS) {
          const res = await listProducts({ tenantId, limit: CATALOG_PAGE_SIZE, offset, enabled: true })
          const page = res.items ?? []
          for (const p of page) items.push(toCatalogItem(p))
          offset += page.length
          if (page.length < CATALOG_PAGE_SIZE || offset >= (res.total ?? 0)) break
        }
        await set(
          catalogKey(tenantId),
          { updated_at: Date.now(), items: items.slice(0, CATALOG_MAX_ITEMS) } satisfies CatalogSnapshot,
          getStore(),
        )
        resolve(true)
      } catch {
        resolve(false)
      }
    })
  }).finally(() => {
    inflight = null
  })
  return inflight
}

/** 仅测试用：清掉模块级状态。 */
export function _resetCatalogForTest(): void {
  store = null
  inflight = null
}
