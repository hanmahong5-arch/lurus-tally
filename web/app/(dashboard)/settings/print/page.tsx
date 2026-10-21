"use client"

import { useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ErrorBanner } from "@/components/ui/error-banner"
import { EMPTY_PRINT_SETTINGS, getPrintSettings, putPrintSettings, type PrintSettings } from "@/lib/api/print-settings"
import { extractApiError } from "@/lib/api/errors"
import { useRole } from "@/hooks/use-role"
import { CloudPrinterCard } from "@/components/print/cloud-printer-card"
import { getAutoPrintReceipt, setAutoPrintReceipt, getReceiptPaper, setReceiptPaper, type ReceiptPaper } from "@/lib/pos/print-prefs"

/**
 * /settings/print: the shop name, phone, address and receipt footer printed on receipts and
 * delivery notes (owner/manager edit them), plus this computer's own choices (everyone).
 */
export default function PrintSettingsPage() {
  const tenantId = useTenantId()
  const { known, can } = useRole()
  const canEditShop = can("settings.manage")
  const [form, setForm] = useState<PrintSettings>(EMPTY_PRINT_SETTINGS)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const [localSaved, setLocalSaved] = useState(false)
  const [autoPrint, setAutoPrint] = useState(false)
  const [paper, setPaper] = useState<ReceiptPaper>("80")

  useAbortableEffect(
    (signal, isCancelled) => {
      setAutoPrint(getAutoPrintReceipt())
      setPaper(getReceiptPaper())
      getPrintSettings(tenantId, signal).then(
        (s) => {
          if (isCancelled()) return
          setForm(s)
          setLoading(false)
        },
        (e) => {
          if (isCancelled() || signal.aborted) return
          setError(extractApiError(e).message)
          setLoading(false)
        }
      )
    },
    [tenantId]
  )

  function set<K extends keyof PrintSettings>(k: K, v: string) {
    setSaved(false)
    setForm((f) => ({ ...f, [k]: v }))
  }

  async function handleSaveShop() {
    setSaving(true)
    setError(null)
    try {
      setForm(await putPrintSettings(form, tenantId))
      setSaved(true)
    } catch (e) {
      setError(extractApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  function handleSaveLocal() {
    setAutoPrintReceipt(autoPrint)
    setReceiptPaper(paper)
    setLocalSaved(true)
  }

  return (
    <PageContainer>
      <PageHeader title="打印设置" subtitle="这里填的内容会印在小票、送货单和采购入库单上" />
      <div className="max-w-xl space-y-4 rounded-xl border border-border bg-card p-4">
        {error && <ErrorBanner>{error}</ErrorBanner>}
        {known && !canEditShop && <p className="text-sm text-muted-foreground">店铺信息由老板或店长填写</p>}
        <div className="space-y-1.5">
          <Label htmlFor="print-shop-name">店名</Label>
          <Input id="print-shop-name" maxLength={64} disabled={loading || !canEditShop} value={form.shop_name} onChange={(e) => set("shop_name", e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="print-phone">联系电话</Label>
          <Input id="print-phone" maxLength={32} disabled={loading || !canEditShop} value={form.phone} onChange={(e) => set("phone", e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="print-address">地址</Label>
          <Input id="print-address" maxLength={128} disabled={loading || !canEditShop} value={form.address} onChange={(e) => set("address", e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="print-footer">小票底部的话</Label>
          <Input
            id="print-footer"
            maxLength={200}
            disabled={loading || !canEditShop}
            placeholder="例如：谢谢惠顾，欢迎再次光临"
            value={form.receipt_footer}
            onChange={(e) => set("receipt_footer", e.target.value)}
          />
        </div>
        {canEditShop && (
          <div className="flex items-center gap-3">
            <Button disabled={loading || saving} onClick={handleSaveShop}>
              {saving ? "保存中…" : "保存店铺信息"}
            </Button>
            {saved && <span className="text-sm text-success">已保存</span>}
          </div>
        )}
      </div>
      <CloudPrinterCard settings={form} loading={loading} onSaved={setForm} />
      <div className="mt-4 max-w-xl space-y-4 rounded-xl border border-border bg-card p-4">
        <p className="text-sm font-medium">这台电脑的打印选项</p>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={autoPrint}
            onChange={(e) => {
              setLocalSaved(false)
              setAutoPrint(e.target.checked)
            }}
          />
          收银结账后自动打印小票（只对这台电脑生效）
        </label>
        <div className="space-y-1.5">
          <Label htmlFor="print-receipt-paper">这台电脑的小票纸宽</Label>
          <select
            id="print-receipt-paper"
            className="h-9 rounded-md border border-border bg-background px-2 text-sm"
            value={paper}
            onChange={(e) => {
              setLocalSaved(false)
              setPaper(e.target.value as ReceiptPaper)
            }}
          >
            <option value="80">80 毫米小票纸</option>
            <option value="58">58 毫米小票纸</option>
          </select>
        </div>
        <div className="flex items-center gap-3">
          <Button onClick={handleSaveLocal}>保存本机选项</Button>
          {localSaved && <span className="text-sm text-success">已保存</span>}
        </div>
      </div>
    </PageContainer>
  )
}
