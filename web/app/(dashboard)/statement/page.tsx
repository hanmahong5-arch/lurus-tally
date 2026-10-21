"use client"

import { Suspense, useState } from "react"
import Link from "next/link"
import { useSearchParams } from "next/navigation"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import {
  getStatement,
  statementCsvUrl,
  statementPrintHref,
  type ReceiptDirection,
  type Statement,
} from "@/lib/api/receipts"
import { extractApiError } from "@/lib/api/errors"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { ErrorBanner } from "@/components/ui/error-banner"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { StatementTable } from "@/components/statement-table"
import { RoleGate } from "@/components/role-gate"

function ymd(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`
}

function monthStart(): string {
  const d = new Date()
  return ymd(new Date(d.getFullYear(), d.getMonth(), 1))
}

function StatementContent() {
  const sp = useSearchParams()
  const partnerId = sp.get("partner") ?? ""
  const direction: ReceiptDirection = sp.get("direction") === "ap" ? "ap" : "ar"
  const tenantId = useTenantId()
  const [from, setFrom] = useState(sp.get("from") ?? monthStart())
  const [to, setTo] = useState(sp.get("to") ?? ymd(new Date()))
  const [st, setSt] = useState<Statement | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useAbortableEffect(
    (signal, isCancelled) => {
      if (!partnerId || !from || !to || to < from) return
      setLoading(true)
      setError(null)
      getStatement(partnerId, { direction, from, to }, { tenantId, signal })
        .then(
          (r) => {
            if (!isCancelled()) setSt(r)
          },
          (e) => {
            if (isCancelled() || signal.aborted) return
            setSt(null)
            setError(extractApiError(e).message)
          }
        )
        .finally(() => {
          if (!isCancelled()) setLoading(false)
        })
    },
    [partnerId, direction, from, to, tenantId]
  )

  const ar = direction === "ar"
  const params = { direction, from, to }

  return (
    <PageContainer width="wide">
      <PageHeader
        title={<span data-testid="page-title">{st ? `${st.partner_name} 对账单` : "对账单"}</span>}
        subtitle={ar ? "客户的发货、退货、收款和欠款" : "供应商的进货、付款和欠款"}
        actions={
          partnerId && st ? (
            <div className="flex items-center gap-3 text-sm">
              <a href={statementCsvUrl(partnerId, params)} className="text-primary hover:underline" download>
                导出表格
              </a>
              <Link
                href={statementPrintHref(direction, partnerId, from, to)}
                target="_blank"
                className="text-primary hover:underline"
              >
                打印
              </Link>
            </div>
          ) : undefined
        }
      />
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <div className="space-y-1.5">
          <Label htmlFor="statement-from">开始日期</Label>
          <Input id="statement-from" type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="statement-to">结束日期</Label>
          <Input id="statement-to" type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} />
        </div>
        <Link href={ar ? "/customers" : "/suppliers"} className="pb-2 text-sm text-muted-foreground hover:underline">
          {ar ? "返回客户列表" : "返回供应商列表"}
        </Link>
      </div>
      {!partnerId && (
        <p className="text-sm text-muted-foreground">请先在{ar ? "客户" : "供应商"}列表里选一家，再看对账单。</p>
      )}
      {error && <ErrorBanner>{error}</ErrorBanner>}
      {loading && !st && <p className="text-sm text-muted-foreground">加载中...</p>}
      {st && <StatementTable st={st} />}
    </PageContainer>
  )
}

export default function StatementPage() {
  return (
    <RoleGate perm="payment.read" title="对账单" notice="对账单只对老板、店长和财务开放" waitForRole>
      <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">加载中...</div>}>
        <StatementContent />
      </Suspense>
    </RoleGate>
  )
}
