"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  listInvoices,
  voidInvoice,
  redInvoice,
  DIRECTION_LABEL,
  INVOICE_STATE_LABEL,
  type Invoice,
  type InvoiceDirection,
  type InvoiceListSummary,
  type InvoiceState,
} from "@/lib/api/invoices"
import { listCustomers } from "@/lib/api/customers"
import { listSuppliers } from "@/lib/api/suppliers"
import { extractApiError } from "@/lib/api/errors"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useConfirm } from "@/hooks/useConfirm"
import { useTenantId } from "@/hooks/use-tenant-id"
import { InvoiceRegisterSheet } from "@/components/invoice-register-sheet"
import { LinkedBills } from "./linked-bills"
import { EntityCombobox, type EntityOption } from "@/components/entity-combobox"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable, currencyCell, statusCell } from "@/components/ui/data-table"
import { Tabs, type TabItem } from "@/components/ui/tabs"
import { Pagination } from "@/components/ui/pagination"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Modal } from "@/components/ui/modal"
import { EmptyState } from "@/components/ui/empty-state"
import { Badge, type BadgeTone } from "@/components/ui/badge"
import { RANGE_PRESETS, RANGE_PRESET_LABEL, resolveDateRange, shanghaiToday, type RangePreset } from "@/lib/date-range"
import { formatCNY, formatDate } from "@/lib/format"
import { cn } from "@/lib/utils"
import { RoleGate } from "@/components/role-gate"

const PAGE_SIZE = 20

const DIRECTION_TABS: TabItem<InvoiceDirection | "">[] = [
  { label: "全部", value: "" },
  { label: "销项（开给客户）", value: "out" },
  { label: "进项（供应商开来）", value: "in" },
]

const STATE_TONE: Record<InvoiceState, BadgeTone> = { issued: "ok", void: "neutral", red: "err" }

const SELECT_CLASS = "h-8 rounded-lg border border-input bg-background px-2 text-sm"

export default function InvoicesPage() {
  return (
    <RoleGate perm="payment.read" title="发票管理" subtitle="登记开出和收到的发票，并对应到销售单、采购单" notice="发票只对老板、店长和财务开放" waitForRole>
      <InvoicesPageInner />
    </RoleGate>
  )
}

function InvoicesPageInner() {
  const tenantId = useTenantId()
  const confirm = useConfirm()

  const [direction, setDirection] = useState<InvoiceDirection | "">("")
  const [status, setStatus] = useState<InvoiceState | "">("")
  const [range, setRange] = useState<RangePreset>("all")
  const [from, setFrom] = useState("")
  const [to, setTo] = useState("")
  const [partner, setPartner] = useState({ id: "", name: "" })
  const [qText, setQText] = useState("")
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [reloadTick, setReloadTick] = useState(0)

  const [items, setItems] = useState<Invoice[]>([])
  const [total, setTotal] = useState(0)
  const [summary, setSummary] = useState<InvoiceListSummary | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [sheetOpen, setSheetOpen] = useState(false)
  const [redTarget, setRedTarget] = useState<Invoice | null>(null)
  const [redNo, setRedNo] = useState("")
  const [redRemark, setRedRemark] = useState("")
  const [redDate, setRedDate] = useState(() => shanghaiToday())
  const [redBusy, setRedBusy] = useState(false)
  const [redError, setRedError] = useState<string | null>(null)

  // Search box: type freely, apply 300ms after the last keystroke.
  const qRef = useRef(q)
  qRef.current = q
  useEffect(() => {
    const next = qText.trim()
    if (next === qRef.current) return
    const t = setTimeout(() => {
      setQ(next)
      setPage(1)
    }, 300)
    return () => clearTimeout(t)
  }, [qText])

  const rangeError = range === "custom" && from && to && from > to ? "开始日期不能晚于结束日期" : ""
  const { from: dateFrom, to: dateTo } = useMemo(() => resolveDateRange(range, new Date(), { from, to }), [range, from, to])

  useAbortableEffect(
    (signal, isCancelled) => {
      if (rangeError) {
        setItems([])
        setTotal(0)
        setSummary(undefined)
        setError(rangeError)
        setLoading(false)
        return
      }
      setLoading(true)
      setError(null)
      listInvoices({
        direction: direction || undefined,
        status: status || undefined,
        partner_id: partner.id || undefined,
        date_from: dateFrom || undefined,
        date_to: dateTo || undefined,
        q: q || undefined,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
        tenantId,
        signal,
        retry: 2,
      })
        .then((res) => {
          if (isCancelled()) return
          setItems(res.items ?? [])
          setTotal(res.total)
          setSummary(res.summary)
        })
        .catch((e) => {
          if (isCancelled() || signal.aborted) return
          setError(extractApiError(e).message)
        })
        .finally(() => {
          if (!isCancelled()) setLoading(false)
        })
    },
    [tenantId, direction, status, partner.id, dateFrom, dateTo, q, page, reloadTick, rangeError],
  )

  const reload = useCallback(() => setReloadTick((n) => n + 1), [])

  // The partner catalogue depends on the direction: customers for 销项, suppliers for 进项.
  const searchPartner = useCallback(
    async (text: string, signal: AbortSignal): Promise<EntityOption[]> => {
      if (direction === "in") {
        const res = await listSuppliers({ q: text || undefined, limit: 20, tenantId, signal })
        return res.items.map((s) => ({ id: s.id, name: s.name, sub: s.phone || s.code || undefined }))
      }
      const res = await listCustomers({ q: text || undefined, limit: 20, tenantId, signal })
      return res.items.map((c) => ({ id: c.id, name: c.name, sub: c.mobile || c.phone || c.code || undefined }))
    },
    [direction, tenantId],
  )

  function changeDirection(d: InvoiceDirection | "") {
    setDirection(d)
    setPartner({ id: "", name: "" })
    setPage(1)
  }

  function clearFilters() {
    setDirection("")
    setStatus("")
    setRange("all")
    setFrom("")
    setTo("")
    setPartner({ id: "", name: "" })
    setQText("")
    setQ("")
    setPage(1)
  }

  async function handleVoid(inv: Invoice) {
    const ok = await confirm({
      title: "作废这张发票",
      body: `发票 ${inv.invoice_no} 将标记为已作废，不再计入已开票金额。适用于发票开错、尚未交付的情况。`,
      confirmText: "确认作废",
      danger: true,
    })
    if (!ok) return
    try {
      await voidInvoice(inv.id, tenantId)
      toast.success(`发票 ${inv.invoice_no} 已作废`)
      reload()
    } catch (e) {
      toast.error("作废失败：" + extractApiError(e).message)
    }
  }

  function openRed(inv: Invoice) {
    setRedTarget(inv)
    setRedNo("")
    setRedRemark("")
    setRedDate(shanghaiToday())
    setRedError(null)
  }

  async function submitRed() {
    if (!redTarget) return
    if (!redNo.trim()) {
      setRedError("请填写红字发票号码")
      return
    }
    if (!redDate) {
      setRedError("请选择红字发票的开票日期")
      return
    }
    setRedBusy(true)
    setRedError(null)
    try {
      await redInvoice(
        redTarget.id,
        { invoice_no: redNo.trim(), issue_date: redDate, remark: redRemark.trim() || undefined },
        tenantId,
      )
      toast.success(`发票 ${redTarget.invoice_no} 已红冲`)
      setRedTarget(null)
      reload()
    } catch (e) {
      setRedError(extractApiError(e).message)
    } finally {
      setRedBusy(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const filtered = !!(direction || status || range !== "all" || partner.id || q)

  const columns: ColumnDef<Invoice>[] = [
    {
      id: "issue_date",
      header: "开票日期",
      cell: ({ row }) => <span className="text-muted-foreground">{formatDate(row.original.issue_date)}</span>,
    },
    {
      id: "direction",
      header: "方向",
      cell: ({ row }) => (
        <Badge tone={row.original.direction === "out" ? "accent" : "info"}>
          {DIRECTION_LABEL[row.original.direction]}
        </Badge>
      ),
    },
    {
      id: "invoice_no",
      header: "发票号码",
      cell: ({ row }) => (
        <>
          <span className="font-mono text-xs">{row.original.invoice_no}</span>
          <span className="block text-xs text-muted-foreground">
            {row.original.invoice_type}
            {row.original.red_of_id ? " · 红字" : ""}
          </span>
        </>
      ),
    },
    {
      id: "partner",
      header: "往来单位",
      cell: ({ row }) => <span>{row.original.partner_name || "—"}</span>,
    },
    {
      id: "linked_bills",
      header: "关联单据",
      meta: { headerClassName: "hidden md:table-cell", cellClassName: "hidden md:table-cell" },
      cell: ({ row }) => <LinkedBills links={row.original.links} />,
    },
    {
      id: "amount_excl_tax",
      header: "不含税金额",
      meta: { align: "right", headerClassName: "hidden md:table-cell", cellClassName: "hidden md:table-cell" },
      cell: ({ row }) => currencyCell(row.original.amount_excl_tax),
    },
    {
      id: "tax_amount",
      header: "税额",
      meta: { align: "right", headerClassName: "hidden md:table-cell", cellClassName: "hidden md:table-cell" },
      cell: ({ row }) => currencyCell(row.original.tax_amount),
    },
    {
      id: "total_amount",
      header: "价税合计",
      meta: { align: "right" },
      cell: ({ row }) => currencyCell(row.original.total_amount),
    },
    {
      id: "status",
      header: "状态",
      cell: ({ row }) => statusCell(INVOICE_STATE_LABEL[row.original.status], STATE_TONE[row.original.status]),
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) =>
        row.original.status === "issued" ? (
          <div className="flex justify-end gap-3">
            {!row.original.red_of_id && (
              <button
                type="button"
                onClick={() => handleVoid(row.original)}
                aria-label={`作废 ${row.original.invoice_no}`}
                className="text-xs text-destructive hover:underline"
              >
                作废
              </button>
            )}
            {!row.original.red_of_id && (
              <button
                type="button"
                onClick={() => openRed(row.original)}
                aria-label={`红冲 ${row.original.invoice_no}`}
                className="text-xs text-primary hover:underline"
              >
                红冲
              </button>
            )}
          </div>
        ) : (
          <span className="block text-right text-muted-foreground">—</span>
        ),
    },
  ]

  return (
    <PageContainer width="wide">
      <PageHeader
        title="发票管理"
        subtitle="登记开出和收到的发票，并对应到销售单、采购单"
        actions={<Button onClick={() => setSheetOpen(true)}>+ 登记发票</Button>}
      />

      <div className="mb-4 space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <div role="group" aria-label="开票时间" className="flex flex-wrap gap-1.5">
            {RANGE_PRESETS.map((p) => (
              <button
                key={p}
                type="button"
                aria-pressed={range === p}
                onClick={() => {
                  if (p === "custom" && range !== "custom" && !from && !to) {
                    const cur = resolveDateRange(range === "all" ? "this_month" : range)
                    setFrom(cur.from)
                    setTo(cur.to)
                  }
                  setRange(p)
                  setPage(1)
                }}
                className={cn(
                  "rounded-full border px-3 py-1 text-xs transition-colors",
                  range === p
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-border bg-background text-muted-foreground hover:bg-muted",
                )}
              >
                {RANGE_PRESET_LABEL[p]}
              </button>
            ))}
          </div>
          {range === "custom" && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Input
                type="date"
                aria-label="开始日期"
                value={from}
                max={to || undefined}
                onChange={(e) => {
                  setFrom(e.target.value)
                  setPage(1)
                }}
                className="w-36"
              />
              <span>至</span>
              <Input
                type="date"
                aria-label="结束日期"
                value={to}
                min={from || undefined}
                onChange={(e) => {
                  setTo(e.target.value)
                  setPage(1)
                }}
                className="w-36"
              />
            </div>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {direction && (
            <EntityCombobox
              id="invoice-filter-partner"
              value={partner.id}
              label={partner.name}
              search={searchPartner}
              entityLabel={direction === "out" ? "客户" : "供应商"}
              placeholder={direction === "out" ? "全部客户" : "全部供应商"}
              onSelect={(o) => {
                setPartner({ id: o.id, name: o.name })
                setPage(1)
              }}
              onClear={() => {
                setPartner({ id: "", name: "" })
                setPage(1)
              }}
              className="w-44"
            />
          )}
          <select
            aria-label="发票状态"
            value={status}
            onChange={(e) => {
              setStatus(e.target.value as InvoiceState | "")
              setPage(1)
            }}
            className={SELECT_CLASS}
          >
            <option value="">全部状态</option>
            <option value="issued">正常</option>
            <option value="void">已作废</option>
            <option value="red">已红冲</option>
          </select>
          <Input
            type="search"
            aria-label="搜索发票号码"
            placeholder="搜发票号码"
            value={qText}
            onChange={(e) => setQText(e.target.value)}
            className="w-56"
          />
        </div>
      </div>

      <Tabs items={DIRECTION_TABS} value={direction} onValueChange={changeDirection} className="mb-4" />

      <p className="mb-3 text-sm text-muted-foreground" data-testid="invoice-summary">
        {summary ? (
          <>
            共 {summary.count} 张 · 不含税 {formatCNY(summary.amount_excl_tax)} · 税额 {formatCNY(summary.tax_amount)} ·
            价税合计 <span className="font-medium text-foreground">{formatCNY(summary.total_amount)}</span>
            <span className="ml-2 text-xs">（已作废的不计入）</span>
          </>
        ) : (
          <>共 {total} 张</>
        )}
      </p>

      <DataTable
        columns={columns}
        data={items}
        loading={loading}
        error={error}
        getRowId={(i) => i.id}
        skeletonRows={5}
        empty={
          <EmptyState
            title="没有找到发票"
            description={filtered ? "换个时间或条件再看看" : "点右上角「登记发票」记下第一张发票"}
            action={
              filtered ? (
                <button type="button" onClick={clearFilters} className="text-sm text-primary hover:underline">
                  清除筛选
                </button>
              ) : undefined
            }
          />
        }
      />

      <Pagination page={page} totalPages={totalPages} onPageChange={setPage} />

      <InvoiceRegisterSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        onCreated={() => {
          setPage(1)
          reload()
        }}
      />

      <Modal
        open={redTarget !== null}
        onOpenChange={(o) => {
          if (!o && !redBusy) setRedTarget(null)
        }}
        title="红冲这张发票"
        description={
          redTarget
            ? `将为发票 ${redTarget.invoice_no}（${formatCNY(redTarget.total_amount)}）登记一张金额相反的红字发票，原发票标记为已红冲，已关联的单据会恢复为未开票。`
            : undefined
        }
        footer={
          <>
            <Button variant="outline" onClick={() => setRedTarget(null)} disabled={redBusy}>
              取消
            </Button>
            <Button variant="destructive" onClick={submitRed} disabled={redBusy}>
              {redBusy ? "处理中..." : "确认红冲"}
            </Button>
          </>
        }
      >
        <div className="mt-4 space-y-3">
          <div className="space-y-1.5">
            <label htmlFor="red-no" className="text-sm font-medium">
              红字发票号码
            </label>
            <Input id="red-no" value={redNo} maxLength={64} onChange={(e) => setRedNo(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="red-date" className="text-sm font-medium">
              红字发票开票日期
            </label>
            <Input id="red-date" type="date" value={redDate} onChange={(e) => setRedDate(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="red-remark" className="text-sm font-medium">
              红冲原因（可选）
            </label>
            <Input id="red-remark" value={redRemark} maxLength={500} onChange={(e) => setRedRemark(e.target.value)} />
          </div>
          {redError && (
            <p role="alert" className="text-sm text-destructive">
              {redError}
            </p>
          )}
        </div>
      </Modal>
    </PageContainer>
  )
}
