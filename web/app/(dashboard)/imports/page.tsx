"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  uploadOrderCSV,
  listMappings,
  type Platform,
  type ImportResult,
  type OversellRow,
  type UnknownSKU,
  type SKUMapping,
} from "@/lib/api/importing"
import { getProduct } from "@/lib/api/products"
import { ApiError, extractApiError } from "@/lib/api/errors"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"

// ----- constants -----------------------------------------------------------

const PLATFORMS: { value: Platform; label: string }[] = [
  { value: "amazon", label: "亚马逊" },
  { value: "shopify", label: "Shopify" },
]

// Skip reasons from the server are codes with optional detail after a colon; show plain Chinese only.
function skipReasonLabel(reason: string): string {
  const head = reason.split(":")[0]
  switch (head) {
    case "duplicate":
      return "这张订单已经导入过"
    case "unknown_sku":
      return "有平台商品编码还没对应到本店商品"
    case "cancelled":
      return "平台上已取消"
    case "approve_failed":
      return "入账失败，请稍后重试"
    default:
      return "已跳过"
  }
}

function platformLabel(value: string): string {
  return PLATFORMS.find((p) => p.value === value)?.label ?? value
}

// At most this many product lookups in flight at once.
const NAME_LOOKUP_CONCURRENCY = 6

// Hard-code a dev warehouse for MVP; production passes it via settings/profile.
// In V2 this becomes a dropdown populated from GET /api/v1/warehouses.
const DEV_WAREHOUSE_ID = process.env.NEXT_PUBLIC_DEFAULT_WAREHOUSE_ID ?? ""

// ----- component -----------------------------------------------------------

/**
 * Imports page — multi-platform CSV order import.
 *
 * Flow:
 *   1. Select platform + upload CSV file.
 *   2. Preview (dry-run) → show oversell rows highlighted.
 *   3. (Optional) map unknown platform codes.
 *   4. Confirm import → show result summary.
 */
export default function ImportsPage() {
  const fileRef = useRef<HTMLInputElement>(null)

  const [platform, setPlatform] = useState<Platform>("amazon")
  const [step, setStep] = useState<"upload" | "preview" | "done">("upload")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [previewResult, setPreviewResult] = useState<ImportResult | null>(null)
  const [finalResult, setFinalResult] = useState<ImportResult | null>(null)

  const [mappings, setMappings] = useState<SKUMapping[]>([])
  const [mappingsLoading, setMappingsLoading] = useState(true)
  // Product names by id; an id absent from the map is still loading.
  const [names, setNames] = useState<ReadonlyMap<string, NameState>>(new Map())
  const requested = useRef(new Set<string>())

  // Load existing platform code mappings on mount.
  useAbortableEffect(
    (signal) => {
      setMappingsLoading(true)
      listMappings(undefined, signal)
        .then((r) => setMappings(r.items))
        .catch(() => {/* non-critical */})
        .finally(() => setMappingsLoading(false))
    },
    [],
  )

  // Look up each distinct product once, so shops with any number of products resolve names.
  const wantedIds = useMemo(() => {
    const ids = new Set<string>()
    for (const m of mappings) ids.add(m.product_id)
    for (const o of previewResult?.oversells ?? []) ids.add(o.product_id)
    for (const o of finalResult?.oversells ?? []) ids.add(o.product_id)
    return Array.from(ids)
  }, [mappings, previewResult, finalResult])

  useEffect(() => {
    const queue = wantedIds.filter((id) => !requested.current.has(id))
    for (const id of queue) requested.current.add(id)
    const seen = requested.current
    let alive = true
    const next = (): void => {
      const id = queue.shift()
      if (id === undefined || !alive) return
      getProduct(id)
        .then((p) => setNames((prev) => new Map(prev).set(id, { kind: "ok", name: p.name })))
        .catch((e) => {
          const notFound = e instanceof ApiError && e.status === 404
          // A failed lookup may be retried the next time the id list changes.
          if (!notFound) seen.delete(id)
          setNames((prev) => new Map(prev).set(id, { kind: notFound ? "missing" : "error" }))
        })
        .finally(next)
    }
    for (let i = 0; i < NAME_LOOKUP_CONCURRENCY; i++) next()
    return () => {
      alive = false
      // Ids not yet started go back to unrequested so a later run picks them up.
      for (const id of queue) seen.delete(id)
    }
  }, [wantedIds])

  // Step 1 → 2: Preview (dry-run).
  const handlePreview = useCallback(async () => {
    const file = fileRef.current?.files?.[0]
    if (!file) {
      setError("请选择 CSV 文件")
      return
    }
    if (!DEV_WAREHOUSE_ID) {
      setError("未配置默认仓库（NEXT_PUBLIC_DEFAULT_WAREHOUSE_ID）")
      return
    }
    setError(null)
    setBusy(true)
    try {
      const result = await uploadOrderCSV(file, platform, DEV_WAREHOUSE_ID, true)
      setPreviewResult(result)
      setStep("preview")
    } catch (e) {
      setError(extractApiError(e).message)
    } finally {
      setBusy(false)
    }
  }, [platform])

  // Step 2 → 3: Confirm import.
  const handleConfirm = useCallback(async () => {
    const file = fileRef.current?.files?.[0]
    if (!file || !DEV_WAREHOUSE_ID) return
    setError(null)
    setBusy(true)
    try {
      const result = await uploadOrderCSV(file, platform, DEV_WAREHOUSE_ID, false)
      setFinalResult(result)
      setStep("done")
    } catch (e) {
      setError(extractApiError(e).message)
    } finally {
      setBusy(false)
    }
  }, [platform])

  const handleReset = useCallback(() => {
    setStep("upload")
    setPreviewResult(null)
    setFinalResult(null)
    setError(null)
    if (fileRef.current) fileRef.current.value = ""
  }, [])

  return (
    <div className="p-6 space-y-6 max-w-4xl mx-auto">
      <div>
        <h1 className="text-2xl font-semibold">订单导入</h1>
        <p className="text-sm text-muted-foreground mt-1">
          将 Amazon / Shopify 订单 CSV 导入为销售单，自动扣减库存
        </p>
      </div>

      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {/* ── Step 1: Upload ── */}
      {step === "upload" && (
        <UploadCard
          platform={platform}
          onPlatformChange={setPlatform}
          fileRef={fileRef}
          busy={busy}
          onPreview={handlePreview}
        />
      )}

      {/* ── Step 2: Preview ── */}
      {step === "preview" && previewResult && (
        <PreviewCard
          result={previewResult}
          busy={busy}
          onConfirm={handleConfirm}
          onBack={handleReset}
          names={names}
        />
      )}

      {/* ── Step 3: Done ── */}
      {step === "done" && finalResult && (
        <ResultCard result={finalResult} onReset={handleReset} />
      )}

      {/* ── Mapping table (always visible below) ── */}
      <MappingsTable mappings={mappings} loading={mappingsLoading} names={names} />
    </div>
  )
}

// ----- sub-components ------------------------------------------------------

function UploadCard({
  platform,
  onPlatformChange,
  fileRef,
  busy,
  onPreview,
}: {
  platform: Platform
  onPlatformChange: (p: Platform) => void
  fileRef: React.RefObject<HTMLInputElement>
  busy: boolean
  onPreview: () => void
}) {
  return (
    <div className="rounded-lg border bg-card p-6 space-y-4">
      <h2 className="font-medium">1. 选择平台和文件</h2>

      <div className="flex gap-3 flex-wrap">
        {PLATFORMS.map((p) => (
          <button
            key={p.value}
            type="button"
            onClick={() => onPlatformChange(p.value)}
            className={`px-4 py-2 rounded-md border text-sm font-medium transition-colors ${
              platform === p.value
                ? "bg-primary text-primary-foreground border-primary"
                : "bg-background hover:bg-muted"
            }`}
          >
            {p.label}
          </button>
        ))}
      </div>

      <div>
        <label className="block text-sm font-medium mb-1" htmlFor="csv-file">
          CSV 文件
        </label>
        <input
          id="csv-file"
          type="file"
          accept=".csv,text/csv"
          ref={fileRef}
          className="block w-full text-sm file:mr-3 file:py-1.5 file:px-3 file:rounded file:border-0 file:text-sm file:font-medium file:bg-muted file:text-foreground hover:file:bg-muted/80 cursor-pointer"
        />
        <p className="text-xs text-muted-foreground mt-1">最大 10 MB</p>
      </div>

      <button
        type="button"
        onClick={onPreview}
        disabled={busy}
        className="px-4 py-2 rounded-md bg-primary text-primary-foreground text-sm font-medium disabled:opacity-50"
      >
        {busy ? "处理中…" : "预览导入"}
      </button>
    </div>
  )
}

type NameState = { kind: "ok"; name: string } | { kind: "missing" } | { kind: "error" }

function nameLabel(s: NameState | undefined): string {
  if (!s) return "加载中"
  if (s.kind === "ok") return s.name
  return s.kind === "missing" ? "未找到商品" : "商品名暂时取不到"
}

function PreviewCard({
  result,
  busy,
  onConfirm,
  onBack,
  names,
}: {
  result: ImportResult
  busy: boolean
  onConfirm: () => void
  onBack: () => void
  names: ReadonlyMap<string, NameState>
}) {
  const hasOversells = result.oversells.length > 0
  const hasUnknown = result.unknown_skus.length > 0

  return (
    <div className="rounded-lg border bg-card p-6 space-y-4">
      <h2 className="font-medium">2. 预览结果</h2>

      <SummaryBadges summary={result.summary} />

      {hasOversells && (
        <OversellTable rows={result.oversells} names={names} />
      )}

      {hasUnknown && (
        <div className="rounded-md bg-amber-50 border border-amber-200 dark:bg-amber-950/30 dark:border-amber-800 p-3">
          <p className="text-sm font-medium text-amber-800 dark:text-amber-200 mb-1">
            以下平台商品编码暂无对应的本店商品，这些订单将被跳过：
          </p>
          <ul className="list-disc list-inside text-sm text-amber-700 dark:text-amber-300 space-y-0.5">
            {result.unknown_skus.map((u: UnknownSKU) => (
              <li key={u.platform_sku}>
                [{platformLabel(u.platform)}] {u.platform_sku}
              </li>
            ))}
          </ul>
          <p className="text-xs text-muted-foreground mt-2">
            请在下方商品编码对应表中添加对应关系后重新上传。
          </p>
        </div>
      )}

      <div className="flex gap-3 pt-2">
        <button
          type="button"
          onClick={onBack}
          disabled={busy}
          className="px-4 py-2 rounded-md border text-sm font-medium hover:bg-muted disabled:opacity-50"
        >
          返回
        </button>
        <button
          type="button"
          onClick={onConfirm}
          disabled={busy || hasOversells}
          className="px-4 py-2 rounded-md bg-primary text-primary-foreground text-sm font-medium disabled:opacity-50"
          title={hasOversells ? "请先解决库存超卖问题" : undefined}
        >
          {busy ? "导入中…" : "确认导入"}
        </button>
      </div>
      {hasOversells && (
        <p className="text-xs text-destructive">存在超卖风险，请先调整库存或修改订单后再确认导入。</p>
      )}
    </div>
  )
}

function ResultCard({
  result,
  onReset,
}: {
  result: ImportResult
  onReset: () => void
}) {
  return (
    <div className="rounded-lg border bg-card p-6 space-y-4">
      <h2 className="font-medium">3. 导入完成</h2>

      <SummaryBadges summary={result.summary} />

      {result.imported.length > 0 && (
        <div className="overflow-x-auto">
          <table className="min-w-full text-sm">
            <thead>
              <tr className="border-b text-left text-muted-foreground">
                <th className="pb-2 pr-4 font-medium">平台单号</th>
                <th className="pb-2 pr-4 font-medium">单据号</th>
              </tr>
            </thead>
            <tbody>
              {result.imported.map((o) => (
                <tr key={o.platform_order_no} className="border-b last:border-0">
                  <td className="py-1.5 pr-4 font-mono text-xs">{o.platform_order_no}</td>
                  <td className="py-1.5 pr-4">{o.bill_no}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {result.skipped.length > 0 && (
        <details className="text-sm">
          <summary className="cursor-pointer text-muted-foreground">
            {result.skipped.length} 条跳过
          </summary>
          <ul className="mt-2 list-disc list-inside space-y-0.5 text-muted-foreground">
            {result.skipped.map((s) => (
              <li key={s.platform_order_no}>
                {s.platform_order_no} — {skipReasonLabel(s.reason)}
              </li>
            ))}
          </ul>
        </details>
      )}

      <button
        type="button"
        onClick={onReset}
        className="px-4 py-2 rounded-md border text-sm font-medium hover:bg-muted"
      >
        继续导入
      </button>
    </div>
  )
}

function SummaryBadges({ summary }: { summary: ImportResult["summary"] }) {
  const badges = [
    { label: "解析", value: summary.total_parsed, color: "bg-muted" },
    { label: "导入", value: summary.imported, color: "bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300" },
    { label: "跳过", value: summary.skipped, color: "bg-muted text-muted-foreground" },
    { label: "超卖", value: summary.oversell_rows, color: summary.oversell_rows > 0 ? "bg-destructive/10 text-destructive" : "bg-muted" },
    { label: "未对应商品编码", value: summary.unknown_skus, color: summary.unknown_skus > 0 ? "bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300" : "bg-muted" },
  ]
  return (
    <div className="flex flex-wrap gap-2">
      {badges.map((b) => (
        <span key={b.label} className={`px-2.5 py-1 rounded-full text-xs font-medium ${b.color}`}>
          {b.label}: {b.value}
        </span>
      ))}
    </div>
  )
}

function OversellTable({ rows, names }: { rows: OversellRow[]; names: ReadonlyMap<string, NameState> }) {
  return (
    <div className="rounded-md border border-destructive/30 overflow-x-auto">
      <div className="px-3 py-2 bg-destructive/5 text-sm font-medium text-destructive border-b border-destructive/30">
        超卖风险 — 以下商品库存不足
      </div>
      <table className="min-w-full text-sm">
        <thead>
          <tr className="border-b text-left text-muted-foreground text-xs">
            <th className="px-3 py-2 font-medium">平台单号</th>
            <th className="px-3 py-2 font-medium">商品</th>
            <th className="px-3 py-2 font-medium text-right">请求数量</th>
            <th className="px-3 py-2 font-medium text-right">可用库存</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr
              key={`${r.platform_order_no}-${i}`}
              className="border-b last:border-0 bg-destructive/5"
            >
              <td className="px-3 py-1.5 font-mono text-xs">{r.platform_order_no}</td>
              <td className="px-3 py-1.5">{nameLabel(names.get(r.product_id))}</td>
              <td className="px-3 py-1.5 text-right">{r.requested_qty}</td>
              <td className="px-3 py-1.5 text-right text-destructive font-medium">{r.available_qty}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function MappingsTable({
  mappings,
  loading,
  names,
}: {
  mappings: SKUMapping[]
  loading: boolean
  names: ReadonlyMap<string, NameState>
}) {
  return (
    <div className="rounded-lg border bg-card p-6 space-y-3">
      <h2 className="font-medium">商品编码对应表</h2>
      <p className="text-sm text-muted-foreground">
        导入时自动学习的平台商品编码与本店商品的对应关系。
      </p>

      {loading ? (
        <p className="text-sm text-muted-foreground">加载中…</p>
      ) : mappings.length === 0 ? (
        <p className="text-sm text-muted-foreground">暂无对应关系，首次导入后自动填充。</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="min-w-full text-sm">
            <thead>
              <tr className="border-b text-left text-muted-foreground text-xs">
                <th className="pb-2 pr-4 font-medium">平台</th>
                <th className="pb-2 pr-4 font-medium">平台商品编码</th>
                <th className="pb-2 pr-4 font-medium">本店商品</th>
                <th className="pb-2 font-medium">更新时间</th>
              </tr>
            </thead>
            <tbody>
              {mappings.map((m) => (
                <tr key={m.id} className="border-b last:border-0">
                  <td className="py-1.5 pr-4">{platformLabel(m.platform)}</td>
                  <td className="py-1.5 pr-4 font-mono text-xs">{m.platform_sku}</td>
                  <td className="py-1.5 pr-4">{nameLabel(names.get(m.product_id))}</td>
                  <td className="py-1.5 text-muted-foreground text-xs">
                    {new Date(m.updated_at).toLocaleDateString("zh-CN")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
