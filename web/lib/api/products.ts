/**
 * API wrapper for the product catalogue endpoints.
 * All functions accept an optional tenantId that is forwarded as X-Tenant-ID header
 * for development; Story 2.1 will replace this with session-cookie auth.
 */
import { apiFetch } from "./client"
import { ApiError, NetworkError } from "./errors"

export type MeasurementStrategy =
  | "individual"
  | "weight"
  | "length"
  | "volume"
  | "batch"
  | "serial"

export interface Product {
  id: string
  tenant_id: string
  category_id?: string
  code: string
  name: string
  manufacturer?: string
  model?: string
  spec?: string
  brand?: string
  mnemonic?: string
  color?: string
  expiry_days?: number
  weight_kg?: string
  enabled: boolean
  enable_serial_no: boolean
  enable_lot_no: boolean
  shelf_position?: string
  img_urls?: string[]
  remark?: string
  measurement_strategy: MeasurementStrategy
  default_unit_id?: string
  /** Default SKU retail price (NUMERIC string); absent when the product has no priced SKU. */
  retail_price?: string
  /** Unit name the retail price is quoted in (from the SKU); compare with the default unit before trusting it. */
  retail_price_unit?: string
  /** Default SKU wholesale / purchase price (NUMERIC strings); absent when the product has no SKU. */
  wholesale_price?: string
  purchase_price?: string
  attributes: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface CreateProductInput {
  /** Optional: the backend generates one (P + yymmdd + 4 chars) when empty. */
  code?: string
  name: string
  category_id?: string
  manufacturer?: string
  model?: string
  spec?: string
  brand?: string
  mnemonic?: string
  color?: string
  expiry_days?: number
  weight_kg?: string
  enable_serial_no?: boolean
  enable_lot_no?: boolean
  shelf_position?: string
  img_urls?: string[]
  remark?: string
  measurement_strategy?: MeasurementStrategy
  default_unit_id?: string
  attributes?: Record<string, unknown>
  /** Default SKU prices (NUMERIC strings). Omit / blank = leave unset (POS then asks for a price). */
  retail_price?: string
  wholesale_price?: string
  purchase_price?: string
}

export interface UpdateProductInput extends Partial<CreateProductInput> {
  enabled?: boolean
}

export interface ListProductsResponse {
  items: Product[]
  total: number
}

export interface ListProductsParams {
  q?: string
  limit?: number
  offset?: number
  enabled?: boolean
  attributes_filter?: Record<string, unknown>
  tenantId?: string
  signal?: AbortSignal
  retry?: number
}

export async function listProducts(
  params: ListProductsParams = {}
): Promise<ListProductsResponse> {
  const { q, limit = 20, offset = 0, enabled, attributes_filter, tenantId, signal, retry } = params
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) usp.set("q", q)
  if (enabled !== undefined) usp.set("enabled", String(enabled))
  if (attributes_filter) usp.set("attributes_filter", JSON.stringify(attributes_filter))
  return apiFetch<ListProductsResponse>(`/products?${usp.toString()}`, { tenantId, signal, retry })
}

export async function getProduct(id: string, tenantId?: string, signal?: AbortSignal): Promise<Product> {
  return apiFetch<Product>(`/products/${id}`, { tenantId, signal })
}

export async function createProduct(
  input: CreateProductInput,
  tenantId?: string
): Promise<Product> {
  return apiFetch<Product>("/products", {
    method: "POST",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function updateProduct(
  id: string,
  input: UpdateProductInput,
  tenantId?: string
): Promise<Product> {
  return apiFetch<Product>(`/products/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
    tenantId,
  })
}

export async function deleteProduct(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/products/${id}`, { method: "DELETE", tenantId })
}

export async function restoreProduct(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/products/${id}/restore`, { method: "POST", tenantId })
}

// ---------------------------------------------------------------------------
// Bulk import (POST /products/import)
// ---------------------------------------------------------------------------

export interface ProductImportRow {
  /** 1-based line in the file; the header row is line 1. */
  line: number
  name: string
  code?: string
  status: "ok" | "error"
  /** Plain-Chinese problems for this row (only when status is "error"). */
  errors?: string[]
  imported: boolean
}

export interface ProductImportResult {
  dry_run: boolean
  total: number
  valid: number
  invalid: number
  imported: number
  /** Units that did not exist yet and are (or would be) created from the file. */
  created_units?: string[]
  rows: ProductImportRow[]
}

/** Header + one example row. The first column is the only required one. */
export const PRODUCT_IMPORT_TEMPLATE =
  "名称*,编码,条码,规格,单位名,零售价,批发价,进价,期初数量,期初成本,仓库\n" +
  "示例：不锈钢螺丝（导入前请改成自己的商品，或删掉这一行）,,\"=\"\"6901234567890\"\"\",M4×20,盒,12.5,10,8,100,8,\n"

/** Template columns only people who may see purchase prices fill in (the server rejects them from anyone else). */
const COST_COLUMNS = ["进价", "期初成本"]

/** Splits one CSV line on commas outside double quotes, keeping the quotes. */
function splitCsvLine(line: string): string[] {
  const cells: string[] = []
  let cur = ""
  let quoted = false
  for (const ch of line) {
    if (ch === '"') quoted = !quoted
    if (ch === "," && !quoted) {
      cells.push(cur)
      cur = ""
    } else {
      cur += ch
    }
  }
  cells.push(cur)
  return cells
}

/** Removes the purchase-price columns (header and every row) from a template. */
function withoutCostColumns(template: string): string {
  const lines = template.split("\n")
  const drop = new Set(
    splitCsvLine(lines[0])
      .map((h, i) => (COST_COLUMNS.includes(h) ? i : -1))
      .filter((i) => i >= 0),
  )
  return lines
    .map((line) =>
      line === ""
        ? line
        : splitCsvLine(line)
            .filter((_, i) => !drop.has(i))
            .join(","),
    )
    .join("\n")
}

/**
 * Same header, with an example row that fits the shop's trade; unknown trades get the hardware row.
 * Without canCost (a clerk) the purchase price and opening cost columns are left out.
 */
export function productImportTemplate(profileType?: string | null, canCost = true): string {
  const template = baseImportTemplate(profileType)
  return canCost ? template : withoutCostColumns(template)
}

function baseImportTemplate(profileType?: string | null): string {
  if (profileType === "horticulture") {
    return (
      "名称*,编码,条码,规格,单位名,零售价,批发价,进价,期初数量,期初成本,仓库\n" +
      "示例：香樟（导入前请改成自己的商品，或删掉这一行）,,,胸径8cm 冠幅2m,株,380,320,200,50,200,\n"
    )
  }
  if (profileType === "cross_border") {
    return (
      "名称*,编码,条码,规格,单位名,零售价,批发价,进价,期初数量,期初成本,仓库\n" +
      "示例：蓝牙耳机（导入前请改成自己的商品，或删掉这一行）,,,黑色 带充电盒,个,59,45,28,200,28,\n"
    )
  }
  return PRODUCT_IMPORT_TEMPLATE
}

const SPREADSHEET_EXT = /\.(xlsx|xlsm|xls)$/i
const HAS_CHINESE = /[一-龥]/

/**
 * Reads a CSV the way Excel saves it in China: UTF-8 (with or without BOM) is used as is,
 * anything else is read as GBK/GB18030. The text is sent on as UTF-8 because the web proxy
 * forwards request bodies as text and would garble raw GBK bytes.
 */
export async function readCsvAsText(file: File): Promise<string> {
  if (SPREADSHEET_EXT.test(file.name)) {
    throw new Error("暂不支持直接上传 Excel 文件，请在 Excel 里选「另存为」，保存类型选「CSV」后再上传")
  }
  const bytes = new Uint8Array(await file.arrayBuffer())
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes)
  } catch {
    return new TextDecoder("gb18030").decode(bytes)
  }
}

/**
 * Uploads the CSV. dryRun=true only checks the file and reports per row; otherwise every
 * valid row is imported. Failures throw ApiError / NetworkError (use extractApiError to show them).
 */
export async function importProducts(
  file: File,
  opts: { dryRun: boolean; tenantId?: string; signal?: AbortSignal; idempotencyKey?: string }
): Promise<ProductImportResult> {
  const text = await readCsvAsText(file)
  const form = new FormData()
  form.append("file", new File([text], file.name, { type: "text/csv" }))
  const headers: Record<string, string> = {
    "Idempotency-Key":
      opts.idempotencyKey ??
      (typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : String(Date.now())),
  }
  if (opts.tenantId) headers["X-Tenant-ID"] = opts.tenantId

  let res: Response
  try {
    res = await fetch(`/api/proxy/products/import${opts.dryRun ? "?dry_run=true" : ""}`, {
      method: "POST",
      body: form,
      headers,
      cache: "no-store",
      signal: opts.signal,
    })
  } catch (err) {
    if (opts.signal?.aborted || (err as Error)?.name === "AbortError") throw err
    throw new NetworkError("network", (err as Error)?.message ?? "network")
  }
  if (!res.ok) {
    let body: { error?: string } = {}
    try {
      body = (await res.json()) as typeof body
    } catch {
      // keep defaults
    }
    const message =
      res.status >= 500
        ? "服务暂时不可用，请稍后重试"
        : res.status === 401
          ? "登录已过期，请刷新页面重新登录后再导入"
          : body.error && HAS_CHINESE.test(body.error)
          ? body.error
          : "导入失败，请检查文件后重试"
    // body stays out of the ApiError: extractApiError would prefer its (English) message.
    throw new ApiError(res.status, "import_failed", message, {})
  }
  return (await res.json()) as ProductImportResult
}

/** UTF-8 byte order mark: makes Excel open a UTF-8 CSV with the right encoding. */
export const CSV_BOM = String.fromCharCode(0xfeff)

function csvCell(v: string): string {
  return /[",\n\r]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v
}

/** CSV (with BOM, opens cleanly in Excel) listing every row that could not be imported and why. */
export function buildImportProblemCsv(result: ProductImportResult): string {
  const lines = ["行号,名称,编码,问题"]
  for (const r of result.rows) {
    if (r.status !== "error") continue
    lines.push([String(r.line), r.name, r.code ?? "", (r.errors ?? []).join("；")].map(csvCell).join(","))
  }
  return CSV_BOM + lines.join("\n") + "\n"
}
