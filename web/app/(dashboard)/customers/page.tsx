"use client"

import { useCallback, useRef, useState } from "react"
import Link from "next/link"
import { getReceivables, unpaidByPartner, openBillsHref } from "@/lib/api/receivables"
import { formatCNY } from "@/lib/format"
import { extractApiError } from "@/lib/api/errors"
import { statementHref } from "@/lib/api/receipts"
import { ReceiptDialog } from "@/components/receipt-dialog"
import { useAbortableEffect } from "@/hooks/useAbortableEffect"
import { useRole } from "@/hooks/use-role"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  listCustomers,
  importCustomers,
  customerImportTemplate,
  createCustomer,
  customerDuplicateOf,
  getCustomer,
  type CustomerItem,
  type CustomerCreateInput,
} from "@/lib/api/customers"
import { CsvImportDialog } from "@/components/csv-import-dialog"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { DataTable } from "@/components/ui/data-table"
import { DataCard } from "@/components/ui/data-card"
import { Pagination } from "@/components/ui/pagination"
import { Sheet } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { EmptyState } from "@/components/ui/empty-state"

const PAGE_SIZE = 20

type CustomerForm = Required<Omit<CustomerCreateInput, "phone">>

const FORM_FIELDS: { key: keyof CustomerForm; label: string; required?: boolean; placeholder: string }[] = [
  { key: "name", label: "名称", required: true, placeholder: "客户名称或姓名" },
  { key: "code", label: "编号", placeholder: "留空自动生成" },
  { key: "contact_name", label: "联系人", placeholder: "可选" },
  { key: "mobile", label: "手机", placeholder: "可选" },
  { key: "address", label: "地址", placeholder: "可选" },
  { key: "remark", label: "备注", placeholder: "可选" },
]

const EMPTY_FORM: CustomerForm = {
  name: "",
  code: "",
  contact_name: "",
  mobile: "",
  address: "",
  remark: "",
}

/**
 * CustomersPage lists the customer catalogue (tally.partner customers) with
 * search, pagination and a create drawer. Customers created inline from a
 * sale bill land here too.
 */
export default function CustomersPage() {
  // 欠款汇总、收款、对账单属于“看收付款”；新增客户需要 customer.write。角色未知时不遮挡，后端兜底。
  const { known, can } = useRole()
  const canMoney = !known || can("payment.read")
  // 请求失败即关闭：会话还没回来时不发受限请求，角色确定后再取。
  const fetchMoney = known && can("payment.read")
  const canCollect = !known || can("payment.record")
  const canCreate = !known || can("customer.write")
  const [items, setItems] = useState<CustomerItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [offset, setOffset] = useState(0)

  // 欠款汇总只取一次；失败时欠款列显示“—”，不影响客户列表本身。
  const [owed, setOwed] = useState<Map<string, string> | null>(null)
  const [owedTick, setOwedTick] = useState(0)
  const canImport = !known || can("catalog.write")
  const [showImport, setShowImport] = useState(false)
  useAbortableEffect((signal, isCancelled) => {
    if (!fetchMoney) return
    getReceivables("ar", { signal })
      .then((r) => {
        if (!isCancelled()) setOwed(unpaidByPartner(r))
      })
      .catch(() => {})
  }, [owedTick, fetchMoney])

  // 整笔收款：先预览会冲掉哪几张单，确认后刷新欠款。
  const [receiptFor, setReceiptFor] = useState<CustomerItem | null>(null)

  const [drawerItem, setDrawerItem] = useState<CustomerItem | null>(null)
  const [showDrawer, setShowDrawer] = useState(false)
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState<CustomerForm>(EMPTY_FORM)
  const [duplicate, setDuplicate] = useState<{ message: string; existingId: string } | null>(null)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(
    (query: string, off: number, signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listCustomers({ q: query || undefined, limit: PAGE_SIZE, offset: off, signal, retry: 2 })
        .then((res) => {
          if (isCancelled?.()) return
          setItems(res.items ?? [])
          setTotal(res.total)
        })
        .catch((e) => {
          if (isCancelled?.() || signal?.aborted) return
          setError(extractApiError(e).message)
        })
        .finally(() => {
          if (isCancelled?.()) return
          setLoading(false)
        })
    },
    []
  )

  useAbortableEffect((signal, isCancelled) => {
    load(q, offset, signal, isCancelled)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [offset])

  function handleSearchChange(value: string) {
    setQ(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      setOffset(0)
      load(value, 0)
    }, 300)
  }

  function openCreate() {
    setDrawerItem(null)
    setForm(EMPTY_FORM)
    setDuplicate(null)
    setShowDrawer(true)
  }

  function closeDrawer() {
    setShowDrawer(false)
    setDrawerItem(null)
  }

  // Opens the already-existing profile in this same drawer (detail view).
  async function showExisting(id: string) {
    try {
      const c = await getCustomer(id)
      setDuplicate(null)
      setDrawerItem(c)
    } catch {
      toast.error("暂时无法打开已有客户，请在列表中搜索")
    }
  }

  async function handleSave() {
    if (!form.name.trim()) {
      toast.error("客户名称不能为空")
      return
    }
    setSaving(true)
    setDuplicate(null)
    try {
      const input: CustomerCreateInput = { name: form.name.trim() }
      for (const { key } of FORM_FIELDS) {
        const v = form[key].trim()
        if (key !== "name" && v) input[key] = v
      }
      await createCustomer(input)
      toast.success("客户已创建")
      closeDrawer()
      load(q, offset)
    } catch (e) {
      const dup = customerDuplicateOf(e)
      if (dup) {
        setDuplicate({ message: dup.message, existingId: dup.existingId })
        return
      }
      toast.error("保存失败：" + extractApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const allColumns: ColumnDef<CustomerItem>[] = [
    {
      id: "name",
      header: "名称",
      cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
    },
    {
      id: "code",
      header: "编号",
      cell: ({ row }) => <span className="text-muted-foreground">{row.original.code || "—"}</span>,
    },
    {
      id: "contact",
      header: "联系人",
      cell: ({ row }) => <span>{row.original.contact_name || "—"}</span>,
    },
    {
      id: "mobile",
      header: "手机",
      cell: ({ row }) => <span>{row.original.mobile || row.original.phone || "—"}</span>,
    },
    {
      id: "owed",
      header: "欠款",
      meta: { align: "right" },
      cell: ({ row }) => {
        if (owed === null) return <span className="block text-right text-muted-foreground">—</span>
        const amt = owed.get(row.original.id)
        if (!amt || Number(amt) <= 0) {
          return <span className="block text-right text-xs text-muted-foreground">无欠款</span>
        }
        return (
          <span className="block text-right" onClick={(e) => e.stopPropagation()}>
            <Link
              href={openBillsHref("ar", row.original.id, row.original.name)}
              className="font-mono tabular-nums text-warning hover:underline"
              title="查看这家的未结清单据"
            >
              {formatCNY(amt)}
            </Link>
          </span>
        )
      },
    },
    {
      id: "actions",
      header: "操作",
      meta: { align: "right" },
      cell: ({ row }) => {
        const amt = owed?.get(row.original.id)
        return (
          <div className="flex justify-end gap-3" onClick={(e) => e.stopPropagation()}>
            {canCollect && amt && Number(amt) > 0 && (
              <button type="button" onClick={() => setReceiptFor(row.original)} className="text-xs text-primary hover:underline">
                收款
              </button>
            )}
            {canMoney && (
              <Link href={statementHref("ar", row.original.id)} className="text-xs text-primary hover:underline">
                对账单
              </Link>
            )}
          </div>
        )
      },
    },
  ]

  // 没有收付款权限的人看不到欠款列；操作列没有内容时也不显示。
  const columns = allColumns.filter((c) => (c.id !== "owed" || canMoney) && (c.id !== "actions" || canMoney))

  return (
    <PageContainer width="wide">
      <PageHeader
        title={<span data-testid="page-title">客户管理</span>}
        subtitle={`共 ${total} 个客户`}
        actions={
          <>
            {canImport && (
              <Button variant="outline" onClick={() => setShowImport(true)}>
                批量导入
              </Button>
            )}
            {canCreate && <Button onClick={openCreate}>+ 新增客户</Button>}
          </>
        }
      />

      <div className="mb-4">
        <Input
          aria-label="搜索客户"
          className="max-w-sm"
          placeholder="搜索客户名称、编号或手机..."
          value={q}
          onChange={(e) => handleSearchChange(e.target.value)}
        />
      </div>

      <DataTable
        columns={columns}
        data={items}
        loading={loading}
        error={error}
        getRowId={(item) => item.id}
        onRowClick={(item) => {
          setDrawerItem(item)
          setShowDrawer(true)
        }}
        mobileCard={(item) => {
          const amt = owed?.get(item.id)
          const hasOwed = !!amt && Number(amt) > 0
          return (
            <DataCard
              title={item.name}
              subtitle={item.code || undefined}
              aside={
                canMoney ? (
                  hasOwed ? (
                    <span className="font-mono tabular-nums text-warning">{formatCNY(amt)}</span>
                  ) : (
                    <span className="text-xs text-muted-foreground">{owed === null ? "—" : "无欠款"}</span>
                  )
                ) : undefined
              }
              fields={[
                { label: "联系人", value: item.contact_name || "—" },
                { label: "手机", value: item.mobile || item.phone || "—" },
              ]}
              actions={
                canMoney ? (
                  <>
                    {canCollect && hasOwed && (
                      <button type="button" onClick={() => setReceiptFor(item)} className="text-xs text-primary hover:underline">
                        收款
                      </button>
                    )}
                    <Link href={statementHref("ar", item.id)} className="text-xs text-primary hover:underline">
                      对账单
                    </Link>
                  </>
                ) : undefined
              }
            />
          )
        }}
        animateRows
        empty={
          <EmptyState
            title="暂无客户"
            description={canCreate ? "在这里添加客户，或在销售单里直接新建" : "还没有客户"}
            action={canCreate ? <Button variant="outline" onClick={openCreate}>新增第一个客户</Button> : undefined}
          />
        }
      />

      <Pagination
        page={currentPage}
        totalPages={totalPages}
        onPageChange={(p) => setOffset((p - 1) * PAGE_SIZE)}
      />

      <Sheet
        open={showDrawer}
        onOpenChange={(o) => {
          if (!o) closeDrawer()
        }}
        title={drawerItem ? drawerItem.name : "新增客户"}
        footer={
          drawerItem ? undefined : (
            <>
              <Button variant="outline" size="sm" onClick={closeDrawer}>
                取消
              </Button>
              <Button size="sm" disabled={saving} onClick={() => void handleSave()}>
                {saving ? "保存中..." : "保存"}
              </Button>
            </>
          )
        }
      >
        {drawerItem ? (
          <div className="flex flex-col gap-3 text-sm" data-testid="customer-drawer">
            {[
              { label: "编号", value: drawerItem.code },
              { label: "联系人", value: drawerItem.contact_name },
              { label: "手机", value: drawerItem.mobile },
              { label: "电话", value: drawerItem.phone },
              { label: "地址", value: drawerItem.address },
              { label: "备注", value: drawerItem.remark },
            ].map(({ label, value }) =>
              value ? (
                <div key={label}>
                  <span className="text-muted-foreground">{label}：</span>
                  <span>{value}</span>
                </div>
              ) : null
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-4" data-testid="customer-drawer">
            {duplicate && (
              <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
                <p>{duplicate.message}</p>
                <button
                  type="button"
                  className="mt-1 text-primary underline underline-offset-2"
                  onClick={() => void showExisting(duplicate.existingId)}
                >
                  查看已有客户
                </button>
              </div>
            )}
            {FORM_FIELDS.map(({ key, label, required, placeholder }) => (
              <div key={key} className="flex flex-col gap-1.5">
                <Label htmlFor={`customer-${key}`}>
                  {label}
                  {required && <span className="text-destructive"> *</span>}
                </Label>
                <Input
                  id={`customer-${key}`}
                  value={form[key]}
                  onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
                  placeholder={placeholder}
                />
              </div>
            ))}
          </div>
        )}
      </Sheet>
      {receiptFor && (
        <ReceiptDialog
          open
          onOpenChange={(o) => {
            if (!o) setReceiptFor(null)
          }}
          direction="ar"
          partnerId={receiptFor.id}
          partnerName={receiptFor.name}
          owed={owed?.get(receiptFor.id)}
          onDone={() => setOwedTick((n) => n + 1)}
        />
      )}
      <CsvImportDialog
        open={showImport}
        onOpenChange={setShowImport}
        noun="客户"
        templateFilename="客户导入模板.csv"
        fetchTemplate={customerImportTemplate}
        importFile={(file, idempotencyKey) => importCustomers(file, { idempotencyKey })}
        onImported={() => load(q, 0)}
      />
    </PageContainer>
  )
}
