"use client"

import { useEffect, useRef, useState } from "react"
import { Modal } from "@/components/ui/modal"
import { Button } from "@/components/ui/button"
import { ErrorBanner } from "@/components/ui/error-banner"
import { extractApiError } from "@/lib/api/errors"
import { readCsvAsText } from "@/lib/api/products"
import { parseCsvPreview, type PartnerImportResult } from "@/lib/api/partner-import"

export interface CsvImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** What is being imported, e.g. "客户" (used in titles and the summary). */
  noun: string
  /** Download name of the template, e.g. "客户导入模板.csv". */
  templateFilename: string
  fetchTemplate: () => Promise<string>
  importFile: (file: File, idempotencyKey: string) => Promise<PartnerImportResult>
  /** Called after at least one row was actually created (to reload the list). */
  onImported: () => void
}

type Phase = "pick" | "preview" | "importing" | "done"

const PREVIEW_ROWS = 20
const MAX_SHOWN_ERRORS = 50

function saveText(filename: string, text: string) {
  const blob = new Blob([text], { type: "text/csv;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * Three-step CSV import shared by customers and suppliers: download the template, pick a file
 * and look at its first rows, then import and read the "新增、跳过、失败" summary.
 */
function skippedText(r: { skipped: number; skipped_by_name?: number; skipped_by_phone?: number }, noun: string): string {
  const phone = r.skipped_by_phone ?? 0
  const name = r.skipped_by_name ?? r.skipped - phone
  const parts: string[] = []
  if (name > 0) parts.push(`${name} 行因名称重复（名称已经存在的${noun}）`)
  if (phone > 0) parts.push(`${phone} 行因手机号重复（手机号已属于另一位${noun}）`)
  return `跳过：${parts.join("；")}。`
}

export function CsvImportDialog({
  open,
  onOpenChange,
  noun,
  templateFilename,
  fetchTemplate,
  importFile,
  onImported,
}: CsvImportDialogProps) {
  const [phase, setPhase] = useState<Phase>("pick")
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<{ header: string[]; rows: string[][]; total: number } | null>(null)
  const [result, setResult] = useState<PartnerImportResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const confirmKey = useRef("")

  useEffect(() => {
    if (open) {
      confirmKey.current = typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : String(Date.now())
      setPhase("pick")
      setFile(null)
      setPreview(null)
      setResult(null)
      setError(null)
    }
  }, [open])

  async function downloadTemplate() {
    setError(null)
    try {
      saveText(templateFilename, await fetchTemplate())
    } catch (e) {
      setError(extractApiError(e).message)
    }
  }

  async function choose(f: File) {
    setError(null)
    try {
      const text = await readCsvAsText(f)
      const p = parseCsvPreview(text, PREVIEW_ROWS)
      if (p.header.length === 0) {
        setError("文件是空的，请先下载模板再填写")
        return
      }
      setFile(f)
      setPreview(p)
      setPhase("preview")
    } catch (e) {
      setError(e instanceof Error ? e.message : extractApiError(e).message)
    }
  }

  async function confirm() {
    if (!file) return
    setError(null)
    setPhase("importing")
    try {
      const res = await importFile(file, confirmKey.current)
      setResult(res)
      setPhase("done")
      if (res.created > 0) onImported()
    } catch (e) {
      setError(extractApiError(e).message)
      setPhase("preview")
    }
  }

  const busy = phase === "importing"

  return (
    <Modal
      open={open}
      onOpenChange={(o) => {
        if (!busy) onOpenChange(o)
      }}
      title={`批量导入${noun}`}
      description={`把表格里的${noun}一次导入。名称相同（客户还包括手机号相同）的${noun}会自动跳过，不会重复新增。`}
      className="max-w-xl"
      footer={
        <>
          {phase === "preview" && (
            <>
              <Button variant="outline" onClick={() => inputRef.current?.click()}>
                重新选择文件
              </Button>
              <Button onClick={confirm}>确认导入</Button>
            </>
          )}
          {phase === "done" && <Button onClick={() => onOpenChange(false)}>完成</Button>}
          {(phase === "pick" || phase === "importing") && (
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
          aria-label={`选择${noun}表格文件`}
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = ""
            if (f) void choose(f)
          }}
        />

        {phase === "pick" && (
          <div className="space-y-3">
            <ol className="list-decimal space-y-1 pl-5 text-muted-foreground">
              <li>
                先
                <button type="button" className="mx-1 text-primary hover:underline" onClick={() => void downloadTemplate()}>
                  下载模板
                </button>
                ，用 Excel 打开，把自己的{noun}填进去（只有「名称」必须填）。
              </li>
              <li>在 Excel 里选「另存为」，保存类型选「CSV」。</li>
              <li>回到这里选择保存好的文件，先看一眼前 {PREVIEW_ROWS} 行，没问题再导入。</li>
            </ol>
            <Button onClick={() => inputRef.current?.click()}>选择文件</Button>
          </div>
        )}

        {phase === "preview" && preview && (
          <div className="space-y-3">
            <p>
              文件共 <span className="font-medium text-foreground">{preview.total}</span> 行数据
              {preview.total > PREVIEW_ROWS ? `，下面只显示前 ${PREVIEW_ROWS} 行` : ""}。
            </p>
            <div className="max-h-64 overflow-auto rounded-lg border">
              <table className="w-full text-left text-xs">
                <thead className="bg-muted/50">
                  <tr>
                    {preview.header.map((h, i) => (
                      <th key={i} className="px-2 py-1.5 font-medium">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {preview.rows.map((r, i) => (
                    <tr key={i} className="border-t">
                      {preview.header.map((_, j) => (
                        <td key={j} className="px-2 py-1.5">
                          {r[j] ?? ""}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {phase === "importing" && <p className="text-muted-foreground">正在导入，请稍候，不要关闭窗口...</p>}

        {phase === "done" && result && (
          <div className="space-y-3">
            <p data-testid="import-summary">
              {`新增 ${result.created}、跳过 ${result.skipped}、失败 ${result.errors.length}。`}
            </p>
            {result.skipped > 0 && (
              <p className="text-muted-foreground" data-testid="import-skipped">
                {skippedText(result, noun)}
              </p>
            )}
            {(result.skipped_rows?.length ?? 0) > 0 && (
              <ul className="max-h-40 space-y-1 overflow-y-auto text-xs text-muted-foreground" data-testid="import-skipped-rows">
                {result.skipped_rows!.slice(0, MAX_SHOWN_ERRORS).map((e, i) => (
                  <li key={i}>
                    第 {e.row} 行：{e.message}
                  </li>
                ))}
              </ul>
            )}
            {result.errors.length > 0 && (
              <ul className="max-h-56 space-y-1.5 overflow-y-auto rounded-lg border border-destructive/30 bg-destructive/5 p-3">
                {result.errors.slice(0, MAX_SHOWN_ERRORS).map((e, i) => (
                  <li key={i}>
                    <span className="font-medium">第 {e.row} 行</span>
                    <span className="text-destructive">：{e.message}</span>
                  </li>
                ))}
                {result.errors.length > MAX_SHOWN_ERRORS && (
                  <li className="text-xs text-muted-foreground">还有 {result.errors.length - MAX_SHOWN_ERRORS} 行没有显示。</li>
                )}
              </ul>
            )}
          </div>
        )}
      </div>
    </Modal>
  )
}
