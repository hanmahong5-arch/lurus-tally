"use client"

import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { useCloudPrint, forgetCloudPrint } from "@/hooks/use-cloud-print"
import { useRole } from "@/hooks/use-role"
import { useTenantId } from "@/hooks/use-tenant-id"
import { getPrinterStatus, printTestPage, type PrinterStatus } from "@/lib/api/print"
import { putPrintSettings, type CloudPrinterSettings, type PrintSettings } from "@/lib/api/print-settings"
import { extractApiError } from "@/lib/api/errors"

const EMPTY: CloudPrinterSettings = { sn: "", key: "", name: "", paper: "58", auto_print: false }

function statusLabel(configured: boolean, s: PrinterStatus | null): string {
  if (!configured) return "未配置"
  if (!s) return "查询中…"
  return s.online ? "在线" : `离线${s.status ? `（${s.status}）` : ""}`
}

/**
 * 「云打印机」卡片：绑定飞鹅云打印机、选纸宽、开关自动打印、打印测试页。
 * 平台没开通云打印（cloud_print_available 为假）时整张卡片不显示；
 * 只有老板和店长（print.settings）能改。
 */
export function CloudPrinterCard({ settings, loading, onSaved }: { settings: PrintSettings; loading: boolean; onSaved: (s: PrintSettings) => void }) {
  const { available } = useCloudPrint()
  const { can } = useRole()
  const canEdit = can("print.settings")
  const tenantId = useTenantId()
  const [form, setForm] = useState<CloudPrinterSettings>(EMPTY)
  const [status, setStatus] = useState<PrinterStatus | null>(null)
  const [busy, setBusy] = useState<"" | "save" | "test">("")
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)

  const savedSN = settings.printer?.sn ?? ""

  useEffect(() => {
    setForm(settings.printer ?? EMPTY)
  }, [settings.printer])

  useEffect(() => {
    if (!available || !savedSN) {
      setStatus(null)
      return
    }
    const ctrl = new AbortController()
    getPrinterStatus({ tenantId, signal: ctrl.signal }).then(
      (s) => setStatus(s),
      () => setStatus({ configured: true, online: false, status: "状态未知，稍后再试", cloud_print_available: true }),
    )
    return () => ctrl.abort()
  }, [available, savedSN, tenantId])

  if (!available) return null

  function set<K extends keyof CloudPrinterSettings>(k: K, v: CloudPrinterSettings[K]) {
    setNote(null)
    setForm((f) => ({ ...f, [k]: v }))
  }

  async function save() {
    setBusy("save")
    setError(null)
    try {
      const saved = await putPrintSettings({ ...settings, printer: form }, tenantId)
      forgetCloudPrint()
      onSaved(saved)
      setNote("已保存")
    } catch (e) {
      setError(extractApiError(e).message)
    } finally {
      setBusy("")
    }
  }

  async function test() {
    setBusy("test")
    setError(null)
    try {
      await printTestPage(tenantId)
      setNote("测试页已发送，请看打印机是否出纸")
    } catch (e) {
      setError(extractApiError(e).message)
    } finally {
      setBusy("")
    }
  }

  return (
    <div className="mt-4 max-w-xl space-y-4 rounded-xl border border-border bg-card p-4" data-testid="cloud-printer-card">
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium">云打印机</p>
        <span className="text-sm text-muted-foreground">状态：{statusLabel(!!savedSN, status)}</span>
      </div>
      {error && <ErrorBanner>{error}</ErrorBanner>}
      {!canEdit && <p className="text-sm text-muted-foreground">云打印机由老板或店长设置</p>}
      <div className="space-y-1.5">
        <Label htmlFor="cloud-printer-sn">打印机编号（机身标签上的 SN）</Label>
        <Input id="cloud-printer-sn" maxLength={32} disabled={loading || !canEdit} value={form.sn} onChange={(e) => set("sn", e.target.value)} />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="cloud-printer-key">打印机密钥（机身标签上的 KEY）</Label>
        <Input id="cloud-printer-key" maxLength={64} disabled={loading || !canEdit} value={form.key} onChange={(e) => set("key", e.target.value)} />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="cloud-printer-name">名称（例如：前台）</Label>
        <Input id="cloud-printer-name" maxLength={32} disabled={loading || !canEdit} value={form.name} onChange={(e) => set("name", e.target.value)} />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="cloud-printer-paper">纸宽</Label>
        <select
          id="cloud-printer-paper"
          className="h-9 rounded-md border border-border bg-background px-2 text-sm"
          disabled={loading || !canEdit}
          value={form.paper}
          onChange={(e) => set("paper", e.target.value === "80" ? "80" : "58")}
        >
          <option value="58">58 毫米小票纸</option>
          <option value="80">80 毫米小票纸</option>
        </select>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" disabled={loading || !canEdit} checked={form.auto_print} onChange={(e) => set("auto_print", e.target.checked)} />
        收银结账后自动打印到云打印机（整个店铺生效）
      </label>
      {canEdit && (
        <div className="flex items-center gap-3">
          <Button disabled={loading || busy !== ""} onClick={save}>
            {busy === "save" ? "保存中…" : "保存云打印机"}
          </Button>
          <Button variant="outline" disabled={loading || busy !== "" || !savedSN} onClick={test}>
            {busy === "test" ? "发送中…" : "打印测试页"}
          </Button>
          {note && <span className="text-sm text-success">{note}</span>}
        </div>
      )}
    </div>
  )
}
