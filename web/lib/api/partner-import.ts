/**
 * Shared client for the customer / supplier bulk import endpoints
 * (POST /{kind}/import and GET /{kind}/import/template.csv).
 */
import { ApiError, NetworkError } from "./errors"
import { readCsvAsText } from "./products"

export interface PartnerImportError {
  row: number
  message: string
}

export interface PartnerImportResult {
  created: number
  skipped: number
  skipped_by_name?: number
  skipped_by_phone?: number
  skipped_rows?: PartnerImportError[]
  errors: PartnerImportError[]
}

const HAS_CHINESE = /[一-龥]/

/** Downloads the blank template (UTF-8 with BOM, two example rows). */
export async function fetchPartnerTemplate(kind: "customers" | "suppliers"): Promise<string> {
  let res: Response
  try {
    res = await fetch(`/api/proxy/${kind}/import/template.csv`, { cache: "no-store" })
  } catch (err) {
    throw new NetworkError("network", (err as Error)?.message ?? "network")
  }
  if (!res.ok) throw new ApiError(res.status, "template_failed", "模板下载失败，请稍后重试", {})
  return await res.text()
}

/** Uploads the CSV; failures throw ApiError / NetworkError (show with extractApiError). */
export async function uploadPartnerCsv(
  kind: "customers" | "suppliers",
  file: File,
  opts: { tenantId?: string; idempotencyKey?: string; signal?: AbortSignal } = {}
): Promise<PartnerImportResult> {
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
    res = await fetch(`/api/proxy/${kind}/import`, {
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
          : res.status === 403
            ? "你的角色没有导入权限"
            : body.error && HAS_CHINESE.test(body.error)
              ? body.error
              : "导入失败，请检查文件后重试"
    throw new ApiError(res.status, "import_failed", message, {})
  }
  const data = (await res.json()) as Partial<PartnerImportResult>
  const skipped = data.skipped ?? 0
  const byPhone = data.skipped_by_phone ?? 0
  return {
    created: data.created ?? 0,
    skipped,
    // Older servers only send the total; treat it as name clashes.
    skipped_by_name: data.skipped_by_name ?? skipped - byPhone,
    skipped_by_phone: byPhone,
    skipped_rows: data.skipped_rows ?? [],
    errors: data.errors ?? [],
  }
}

/** Minimal CSV reader for the preview table (quotes, doubled quotes, CRLF). */
export function parseCsvPreview(text: string, maxRows: number): { header: string[]; rows: string[][]; total: number } {
  const all: string[][] = []
  let row: string[] = []
  let cell = ""
  let quoted = false
  const src = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text
  for (let i = 0; i < src.length; i++) {
    const ch = src[i]
    if (quoted) {
      if (ch === '"') {
        if (src[i + 1] === '"') {
          cell += '"'
          i++
        } else quoted = false
      } else cell += ch
    } else if (ch === '"') quoted = true
    else if (ch === ",") {
      row.push(cell)
      cell = ""
    } else if (ch === "\n" || ch === "\r") {
      if (ch === "\r" && src[i + 1] === "\n") i++
      row.push(cell)
      cell = ""
      all.push(row)
      row = []
    } else cell += ch
  }
  if (cell !== "" || row.length > 0) {
    row.push(cell)
    all.push(row)
  }
  const nonBlank = all.filter((r) => r.some((c) => c.trim() !== ""))
  const [header = [], ...rest] = nonBlank
  return { header: header.map((h) => h.trim()), rows: rest.slice(0, maxRows), total: rest.length }
}
