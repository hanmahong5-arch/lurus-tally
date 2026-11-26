"use client"

import { useState } from "react"
import { toast } from "sonner"

import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { RoleGate } from "@/components/role-gate"
import { formatCNY, formatDateTime } from "@/lib/format"
import { extractApiError } from "@/lib/api/errors"
import { ignoreMoneyLine, importMoneyFile, listMoneyLines, matchMoneyLine, type MoneyLine } from "@/lib/api/money"

function fileToBase64(file: File): Promise<string> {
  return file.arrayBuffer().then((buf) => {
    const bytes = new Uint8Array(buf)
    let bin = ""
    for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
    return btoa(bin)
  })
}

function channelLabel(channel: string): string {
  if (channel === "wechat") return "微信"
  if (channel === "alipay") return "支付宝"
  return "银行"
}

function moneyText(amount: string): string {
  const n = Number(amount)
  const text = formatCNY(Math.abs(n))
  return n < 0 ? `付款 ${text}` : `收款 ${text}`
}

function MoneyPageContent() {
  const [lines, setLines] = useState<MoneyLine[]>([])
  const [error, setError] = useState("")
  const [account, setAccount] = useState("")
  const [busy, setBusy] = useState("")

  const reload = (signal?: AbortSignal) => {
    listMoneyLines(signal)
      .then((res) => setLines(res.items ?? []))
      .catch((err) => {
        if (signal?.aborted) return
        setError(extractApiError(err).message)
      })
  }

  useAbortableEffect((signal) => {
    reload(signal)
  }, [])

  async function onFile(file: File | undefined) {
    if (!file) return
    setError("")
    setBusy("upload")
    try {
      const result = await importMoneyFile(await fileToBase64(file), account.trim())
      toast.success(`写入 ${result.inserted} 笔，跳过 ${result.skipped} 笔重复流水`)
      reload()
    } catch (err) {
      setError(extractApiError(err).message)
    } finally {
      setBusy("")
    }
  }

  async function onMatch(line: MoneyLine, billId: string) {
    setBusy(line.id + billId)
    setError("")
    try {
      await matchMoneyLine(line.id, billId)
      toast.success("已记到这张单")
      reload()
    } catch (err) {
      setError(extractApiError(err).message)
    } finally {
      setBusy("")
    }
  }

  async function onIgnore(line: MoneyLine) {
    setBusy(line.id)
    setError("")
    try {
      await ignoreMoneyLine(line.id)
      reload()
    } catch (err) {
      setError(extractApiError(err).message)
    } finally {
      setBusy("")
    }
  }

  return (
    <PageContainer width="default">
      <PageHeader
        title="流水"
        subtitle="上传支付宝或微信导出的 csv。金额和未结清的单据一样时才会列出来。确认后才记收付款，不改库存。"
      />
      {error && <ErrorBanner>{error}</ErrorBanner>}
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end">
        <label className="flex flex-col gap-1 text-sm">
          商户号
          <Input value={account} maxLength={64} placeholder="同一个渠道有多个账号时填写" onChange={(e) => setAccount(e.target.value)} />
        </label>
        <label className="inline-flex h-8 cursor-pointer items-center rounded-lg bg-primary px-2.5 text-sm font-medium text-primary-foreground">
          {busy === "upload" ? "正在导入…" : "上传账单"}
          <input
            type="file"
            accept=".csv,text/csv,.txt"
            className="sr-only"
            disabled={busy === "upload"}
            onChange={(e) => {
              const file = e.target.files?.[0]
              e.target.value = ""
              void onFile(file)
            }}
          />
        </label>
      </div>
      {lines.length === 0 && <p className="text-sm text-muted-foreground">没有待入账的流水。</p>}
      <ul className="flex flex-col gap-3">
        {lines.map((line) => (
          <li key={line.id} className="rounded-lg border border-border p-3">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <div className="text-sm">
                <span className="font-medium">{channelLabel(line.channel)}</span>
                <span className="mx-2 text-muted-foreground">{formatDateTime(line.booked_at)}</span>
                <span>{line.counterparty || "对方未写在账单里"}</span>
              </div>
              <div className="font-medium">{moneyText(line.amount)}</div>
            </div>
            {line.memo && <p className="mt-1 text-sm text-muted-foreground">{line.memo}</p>}
            <div className="mt-3 flex flex-wrap gap-2">
              {line.candidates.map((c) => (
                <Button key={c.bill_id} size="sm" disabled={busy !== ""} onClick={() => void onMatch(line, c.bill_id)}>
                  记到这张单 {c.bill_no}（{c.partner_name}）
                </Button>
              ))}
              {line.candidates.length === 0 && (
                <span className="text-sm text-muted-foreground">没有金额相同的未结单据。</span>
              )}
              <Button size="sm" variant="outline" disabled={busy !== ""} onClick={() => void onIgnore(line)}>
                先不入账
              </Button>
            </div>
          </li>
        ))}
      </ul>
    </PageContainer>
  )
}

export default function MoneyPage() {
  return (
    <RoleGate perm="payment.read" title="流水" notice="流水只对老板、店长和财务开放" waitForRole>
      <MoneyPageContent />
    </RoleGate>
  )
}
