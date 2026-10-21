"use client"

import { useEffect, useRef, useState } from "react"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { ErrorBanner } from "@/components/ui/error-banner"
import { extractApiError } from "@/lib/api/errors"
import { useProfile } from "@/lib/profile"
import { useRole } from "@/hooks/use-role"
import {
  CSV_BOM,
  productImportTemplate,
  buildImportProblemCsv,
  importProducts,
  type ProductImportResult,
} from "@/lib/api/products"

export interface ProductImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  tenantId?: string
  /** Called after products were actually imported (to reload the list). */
  onImported: () => void
}

type Phase = "pick" | "checking" | "preview" | "importing" | "done"

/** Shown problem rows are capped so a bad 5000-row file does not freeze the dialog. */
const MAX_SHOWN_PROBLEMS = 50

function downloadCsv(filename: string, text: string) {
  const blob = new Blob([text], { type: "text/csv;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * Bulk product import for shops moving off paper or Excel: pick a CSV, see what would happen
 * (row by row, in plain words), then confirm. Rows with an opening quantity also get their
 * starting stock; nothing here creates a purchase bill or a payable.
 */
export function ProductImportDialog({ open, onOpenChange, tenantId, onImported }: ProductImportDialogProps) {
  const { profileType } = useProfile()
  // The purchase-price columns are only offered to people who may see purchase prices.
  const { can } = useRole()
  const canCost = can("cost.view")
  const [file, setFile] = useState<File | null>(null)
  const [phase, setPhase] = useState<Phase>("pick")
  const [result, setResult] = useState<ProductImportResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  // One key per confirmation: a retry of the same upload reuses it.
  const confirmKey = useRef<string>("")

  useEffect(() => {
    if (open) {
      confirmKey.current = typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : String(Date.now())
      setFile(null)
      setPhase("pick")
      setResult(null)
      setError(null)
    }
  }, [open])

  async function check(f: File) {
    setFile(f)
    setError(null)
    setPhase("checking")
    try {
      setResult(await importProducts(f, { dryRun: true, tenantId }))
      setPhase("preview")
    } catch (e) {
      setError(extractApiError(e).message)
      setPhase("pick")
    }
  }

  async function confirm() {
    if (!file) return
    setError(null)
    setPhase("importing")
    try {
      const res = await importProducts(file, { dryRun: false, tenantId, idempotencyKey: confirmKey.current })
      setResult(res)
      setPhase("done")
      if (res.imported > 0) onImported()
    } catch (e) {
      setError(extractApiError(e).message)
      setPhase("preview")
    }
  }

  const problems = result?.rows.filter((r) => r.status === "error") ?? []
  const busy = phase === "checking" || phase === "importing"

  return (
    <Modal
      open={open}
      onOpenChange={(o) => {
        if (!busy) onOpenChange(o)
      }}
      title="批量导入商品"
      description="把表格里的商品一次导入，带「期初数量」的商品会同时建好库存。导入不会产生采购单，也不会产生应付款。"
      className="max-w-xl"
      footer={
        <>
          {phase === "preview" && (
            <>
              <Button variant="outline" onClick={() => inputRef.current?.click()}>
                重新选择文件
              </Button>
              <Button disabled={!result || result.valid === 0} onClick={confirm}>
                {result && result.valid > 0 ? `确认导入 ${result.valid} 个商品` : "没有可导入的商品"}
              </Button>
            </>
          )}
          {phase === "done" && <Button onClick={() => onOpenChange(false)}>完成</Button>}
          {(phase === "pick" || phase === "checking" || phase === "importing") && (
            <Button variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
              取消
            </Button>
          )}
        </>
      }
    >
      <div className="mt-4 space-y-3 text-sm">
        {error && <ErrorBanner>{error}</ErrorBanner>}

        <input
          ref={inputRef}
          type="file"
          accept=".csv,text/csv"
          aria-label="选择商品表格文件"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = ""
            if (f) void check(f)
          }}
        />

        {(phase === "pick" || phase === "checking") && (
          <div className="space-y-3">
            <ol className="list-decimal space-y-1 pl-5 text-muted-foreground">
              <li>
                先
                <button
                  type="button"
                  className="mx-1 text-primary hover:underline"
                  onClick={() => downloadCsv("商品导入模板.csv", CSV_BOM + productImportTemplate(profileType, canCost))}
                >
                  下载模板
                </button>
                ，用 Excel 打开，把自己的商品填进去（只有「名称」必须填）。
              </li>
              <li>在 Excel 里选「另存为」，保存类型选「CSV」。</li>
              <li>回到这里选择保存好的文件，我们会先检查一遍，确认没问题再导入。</li>
            </ol>
            <Button disabled={busy} onClick={() => inputRef.current?.click()}>
              {phase === "checking" ? "正在检查..." : "选择文件"}
            </Button>
          </div>
        )}

        {phase === "preview" && result && (
          <div className="space-y-3">
            <p>
              共 {result.total} 行：<span className="font-medium text-foreground">{result.valid} 行可以导入</span>
              {result.invalid > 0 && <span className="text-destructive">，{result.invalid} 行有问题（不会导入）</span>}。
            </p>
            {result.created_units && result.created_units.length > 0 && (
              <p className="text-muted-foreground">
                导入时会自动新建这些单位：{result.created_units.join("、")}。
              </p>
            )}
            {problems.length > 0 && <ProblemList result={result} />}
          </div>
        )}

        {phase === "importing" && <p className="text-muted-foreground">正在导入，请稍候，不要关闭窗口...</p>}

        {phase === "done" && result && (
          <div className="space-y-3">
            <p>
              已导入 <span className="font-medium text-foreground">{result.imported}</span> 个商品
              {result.invalid > 0 && <span className="text-destructive">，{result.invalid} 行没有导入</span>}。
            </p>
            {problems.length > 0 && <ProblemList result={result} />}
          </div>
        )}
      </div>
    </Modal>
  )
}

function ProblemList({ result }: { result: ProductImportResult }) {
  const problems = result.rows.filter((r) => r.status === "error")
  return (
    <div className="space-y-2">
      <ul className="max-h-56 space-y-1.5 overflow-y-auto rounded-lg border border-destructive/30 bg-destructive/5 p-3">
        {problems.slice(0, MAX_SHOWN_PROBLEMS).map((r) => (
          <li key={r.line}>
            <span className="font-medium">
              第 {r.line} 行{r.name ? `（${r.name}）` : ""}
            </span>
            <span className="text-destructive">：{(r.errors ?? []).join("；")}</span>
          </li>
        ))}
      </ul>
      {problems.length > MAX_SHOWN_PROBLEMS && (
        <p className="text-xs text-muted-foreground">
          还有 {problems.length - MAX_SHOWN_PROBLEMS} 行问题没有显示，请下载问题清单查看全部。
        </p>
      )}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => downloadCsv("导入问题清单.csv", buildImportProblemCsv(result))}
      >
        下载问题清单
      </Button>
    </div>
  )
}
