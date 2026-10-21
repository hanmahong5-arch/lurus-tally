"use client"

import { useCallback, useMemo, useState } from "react"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useTenantId } from "@/hooks/use-tenant-id"
import { InvoiceRegisterSheet, type InvoicePreset } from "@/components/invoice-register-sheet"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { listInvoices, moneyGt, remainingToInvoice, sumMoney, type Invoice, type InvoiceDirection } from "@/lib/api/invoices"
import { formatCNY, formatDate } from "@/lib/format"

/** One page is plenty: a bill rarely carries more than a handful of live invoices. */
const PAGE_SIZE = 100

interface Props {
  kind: "sale" | "purchase"
  billId: string
  partnerId?: string
  partnerName?: string
  /** Invoices only attach to approved bills. */
  approved: boolean
  billNo?: string
  billDate?: string
  /** Bill total; with the linked amounts it decides whether anything is left to register. */
  totalAmount?: string
  /** Sales returns already taken off this bill; the invoiceable total is net of it. */
  returnedAmount?: string
}

/**
 * Bill-detail panel: invoices registered against this bill, plus a shortcut that
 * opens the 登记发票 drawer with this bill already ticked.
 */
export function BillInvoices({ kind, billId, partnerId, partnerName, approved, billNo, billDate, totalAmount, returnedAmount }: Props) {
  const tenantId = useTenantId()
  const direction: InvoiceDirection = kind === "sale" ? "out" : "in"
  const [rows, setRows] = useState<{ invoice: Invoice; amount: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [truncated, setTruncated] = useState(false)
  const [open, setOpen] = useState(false)
  // The drawer resets its form whenever its preset changes identity. Freeze the
  // preset at open time so the invoice list finishing its load (which changes
  // `remaining`) cannot wipe what the user has already typed.
  const [frozenPreset, setFrozenPreset] = useState<InvoicePreset | undefined>(undefined)
  const [tick, setTick] = useState(0)

  useAbortableEffect(
    (signal, isCancelled) => {
      if (!approved || !partnerId) return
      setLoading(true)
      setError(null)
      listInvoices({ bill_id: billId, status: "issued", limit: PAGE_SIZE, tenantId, signal })
        .then((res) => {
          if (isCancelled()) return
          const mine: { invoice: Invoice; amount: string }[] = []
          for (const inv of res.items ?? []) {
            for (const l of (inv.links ?? []).filter((x) => x.bill_id === billId)) mine.push({ invoice: inv, amount: l.amount })
          }
          setRows(mine)
          setTruncated(res.total > (res.items ?? []).length)
        })
        .catch((e) => {
          if (isCancelled() || signal.aborted) return
          setError("发票信息加载失败，请刷新页面重试")
          console.error(e)
        })
        .finally(() => {
          if (!isCancelled()) setLoading(false)
        })
    },
    [approved, partnerId, direction, billId, tenantId, tick],
  )

  // Derived on the client from the linked amounts (decimal strings, never float sums).
  const remaining = useMemo(
    () =>
      totalAmount
        ? remainingToInvoice(remainingToInvoice(totalAmount, returnedAmount), sumMoney(rows.map((r) => r.amount)))
        : undefined,
    [totalAmount, returnedAmount, rows],
  )
  const fullyInvoiced = !loading && !error && remaining !== undefined && !moneyGt(remaining, "0")
  const preset = useMemo<InvoicePreset | undefined>(
    () =>
      partnerId
        ? {
            direction,
            partnerId,
            partnerName: partnerName ?? "",
            billId,
            bill:
              billNo && billDate && totalAmount && remaining !== undefined
                ? { billNo, billDate, totalAmount, remaining }
                : undefined,
          }
        : undefined,
    [direction, partnerId, partnerName, billId, billNo, billDate, totalAmount, remaining],
  )
  const onCreated = useCallback(() => setTick((n) => n + 1), [])

  if (!approved) return null

  return (
    <div className="rounded-xl border border-border bg-card p-4" data-testid="bill-invoices">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-medium text-muted-foreground">关联发票</h2>
        {preset && !fullyInvoiced && (
          <Button
            size="sm"
            variant="outline"
            disabled={loading}
            onClick={() => {
              setFrozenPreset(preset)
              setOpen(true)
            }}
          >
            登记发票
          </Button>
        )}
      </div>
      {!partnerId ? (
        <p className="text-sm text-muted-foreground">这张单据没有指定{kind === "sale" ? "客户" : "供应商"}，不能登记发票</p>
      ) : loading ? (
        <p className="text-sm text-muted-foreground">加载中...</p>
      ) : error ? (
        <p className="text-sm text-destructive">{error}</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {truncated ? "只核对了最近的一部分发票，没有找到关联本单的发票，更早的发票可能没有显示" : "还没有登记发票"}
        </p>
      ) : (
        <ul className="divide-y divide-border text-sm">
          {rows.map(({ invoice, amount }) => (
            <li key={invoice.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2">
              <span className="font-mono text-xs">{invoice.invoice_no}</span>
              <Badge tone="neutral">{invoice.invoice_type}</Badge>
              <span className="text-muted-foreground">{formatDate(invoice.issue_date)}</span>
              <span className="ml-auto font-mono tabular-nums">本单关联 {formatCNY(amount)}</span>
            </li>
          ))}
        </ul>
      )}
      {truncated && rows.length > 0 && (
        <p className="mt-2 text-xs text-muted-foreground">只核对了最近的一部分发票，更早的发票可能没有显示</p>
      )}
      {preset && (
        <InvoiceRegisterSheet open={open} onOpenChange={setOpen} preset={open ? (frozenPreset ?? preset) : preset} onCreated={onCreated} />
      )}
    </div>
  )
}
