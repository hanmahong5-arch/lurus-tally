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
  listSuppliers,
  importSuppliers,
  supplierImportTemplate,
  deleteSupplier,
  createSupplier,
  updateSupplier,
  type SupplierItem,
  type SupplierCreateInput,
} from "@/lib/api/suppliers"
import { useConfirm } from "@/hooks/useConfirm"
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

interface SupplierForm {
  name: string
  code: string
  contact: string
  phone: string
  email: string
  address: string
  remark: string
}

const FORM_FIELDS: { key: keyof SupplierForm; label: string; required?: boolean; placeholder: string }[] = [
  { key: "name", label: "名称", required: true, placeholder: "供应商名称" },
  { key: "code", label: "编号", placeholder: "留空自动生成" },
  { key: "contact", label: "联系人", placeholder: "可选" },
  { key: "phone", label: "电话", placeholder: "可选" },
  { key: "email", label: "邮箱", placeholder: "可选" },
  { key: "address", label: "地址", placeholder: "可选" },
  { key: "remark", label: "备注", placeholder: "可选" },
]

const EMPTY_FORM: SupplierForm = {
  name: "",
  code: "",
  contact: "",
  phone: "",
  email: "",
  address: "",
  remark: "",
}

/**
 * SuppliersPage renders the supplier list with search, pagination, and
 * a create/edit/view drawer (W3.D1).
 */
export default function SuppliersPage() {
  // 欠款、付款、对账单属于“看收付款”；增删改供应商需要 catalog.write。角色未知时不遮挡，后端兜底。
  const { known, can } = useRole()
  const canMoney = !known || can("payment.read")
  // 请求失败即关闭：会话还没回来时不发受限请求，角色确定后再取。
  const fetchMoney = known && can("payment.read")
  const canPay = !known || can("payment.record")
  const canEdit = !known || can("catalog.write")
  const [items, setItems] = useState<SupplierItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState("")
  const [offset, setOffset] = useState(0)

  // 欠款汇总只取一次；失败时欠款列显示“—”，不影响供应商列表本身。
  const [owed, setOwed] = useState<Map<string, string> | null>(null)
  const [owedTick, setOwedTick] = useState(0)
  const [showImport, setShowImport] = useState(false)
  useAbortableEffect((signal, isCancelled) => {
    if (!fetchMoney) return
    getReceivables("ap", { signal })
      .then((r) => {
        if (!isCancelled()) setOwed(unpaidByPartner(r))
      })
      .catch(() => {})
  }, [owedTick, fetchMoney])

  // 整笔付款：先预览会付清哪几张进货单，确认后刷新欠款。
  const [payFor, setPayFor] = useState<SupplierItem | null>(null)

  const [drawerItem, setDrawerItem] = useState<SupplierItem | null>(null)
  const [drawerMode, setDrawerMode] = useState<"view" | "edit" | "create">("view")
  const [showDrawer, setShowDrawer] = useState(false)
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState<SupplierForm>(EMPTY_FORM)

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const confirm = useConfirm()

  const load = useCallback(
    (query: string, off: number, signal?: AbortSignal, isCancelled?: () => boolean) => {
      setLoading(true)
      setError(null)
      listSuppliers({ q: query || undefined, limit: PAGE_SIZE, offset: off, signal, retry: 2 })
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
    setDrawerMode("create")
    setForm(EMPTY_FORM)
    setShowDrawer(true)
  }

  function openDetail(item: SupplierItem) {
    setDrawerItem(item)
    setDrawerMode("view")
    setShowDrawer(true)
  }

  function openEdit(item: SupplierItem) {
    setDrawerItem(item)
    setDrawerMode("edit")
    setForm({
      name: item.name,
      code: item.code,
      contact: item.contact,
      phone: item.phone,
      email: item.email,
      address: item.address,
      remark: item.remark,
    })
    setShowDrawer(true)
  }

  function closeDrawer() {
    setShowDrawer(false)
    setDrawerItem(null)
  }

  async function handleSave() {
    if (!form.name.trim()) {
      toast.error("供应商名称不能为空")
      return
    }
    setSaving(true)
    try {
      const input: SupplierCreateInput = {
        name: form.name.trim(),
        code: form.code.trim() || undefined,
        contact: form.contact.trim() || undefined,
        phone: form.phone.trim() || undefined,
        email: form.email.trim() || undefined,
        address: form.address.trim() || undefined,
        remark: form.remark.trim() || undefined,
      }
      if (drawerMode === "create") {
        await createSupplier(input)
        toast.success("供应商已创建")
      } else if (drawerItem) {
        await updateSupplier(drawerItem.id, input)
        toast.success("供应商已更新")
      }
      closeDrawer()
      load(q, offset)
    } catch (e) {
      toast.error("保存失败：" + extractApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(item: SupplierItem) {
    const ok = await confirm({
      title: "删除供应商",
      body: `确认删除「${item.name}」？删除后列表里不再显示，如需找回请联系客服。`,
      confirmText: "删除",
      danger: true,
    })
    if (!ok) return
    try {
      await deleteSupplier(item.id)
      load(q, offset)
    } catch (e) {
      toast.error("删除失败：" + extractApiError(e).message)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const allColumns: ColumnDef<SupplierItem>[] = [
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
      cell: ({ row }) => <span>{row.original.contact || "—"}</span>,
    },
    {
      id: "phone",
      header: "电话",
      cell: ({ row }) => <span>{row.original.phone || "—"}</span>,
    },
    {
      id: "owed",
      header: "欠供应商款",
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
              href={openBillsHref("ap", row.original.id, row.original.name)}
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
      cell: ({ row }) => (
        <div className="flex justify-end gap-3" onClick={(e) => e.stopPropagation()}>
          {canPay && Number(owed?.get(row.original.id) ?? 0) > 0 && (
            <button type="button" onClick={() => setPayFor(row.original)} className="text-xs text-primary hover:underline">
              付款
            </button>
          )}
          {canMoney && (
            <Link href={statementHref("ap", row.original.id)} className="text-xs text-primary hover:underline">
              对账单
            </Link>
          )}
          {canEdit && (
            <>
              <button
                type="button"
                onClick={() => openEdit(row.original)}
                className="text-xs text-primary hover:underline"
              >
                编辑
              </button>
              <button
                type="button"
                onClick={() => void handleDelete(row.original)}
                className="text-xs text-destructive hover:underline"
              >
                删除
              </button>
            </>
          )}
        </div>
      ),
    },
  ]

  const isForm = drawerMode === "create" || drawerMode === "edit"
  // 没有收付款权限的人看不到欠款列；既不能付款、看对账单、也不能改的人，操作列没有内容。
  const columns = allColumns.filter(
    (c) => (c.id !== "owed" || canMoney) && (c.id !== "actions" || canMoney || canEdit),
  )

  return (
    <PageContainer width="wide">
      <PageHeader
        title={<span data-testid="page-title">供应商管理</span>}
        subtitle={`共 ${total} 个供应商`}
        actions={
          <>
            {canEdit && (
              <Button variant="outline" onClick={() => setShowImport(true)}>
                批量导入
              </Button>
            )}
            {canEdit && <Button onClick={openCreate}>+ 新增供应商</Button>}
          </>
        }
      />

      <div className="mb-4">
        <Input
          aria-label="搜索供应商"
          className="max-w-sm"
          placeholder="搜索供应商名称或编号..."
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
        onRowClick={openDetail}
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
                { label: "联系人", value: item.contact || "—" },
                { label: "电话", value: item.phone || "—" },
              ]}
              actions={
                <>
                  {canPay && hasOwed && (
                    <button type="button" onClick={() => setPayFor(item)} className="text-xs text-primary hover:underline">
                      付款
                    </button>
                  )}
                  {canMoney && (
                    <Link href={statementHref("ap", item.id)} className="text-xs text-primary hover:underline">
                      对账单
                    </Link>
                  )}
                  {canEdit && (
                    <button type="button" onClick={() => openEdit(item)} className="text-xs text-primary hover:underline">
                      编辑
                    </button>
                  )}
                </>
              }
            />
          )
        }}
        animateRows
        empty={
          <EmptyState
            title="暂无供应商"
            description={canEdit ? "添加供应商后可在采购单中快速引用" : "还没有供应商"}
            action={canEdit ? <Button variant="outline" onClick={openCreate}>新增第一个供应商</Button> : undefined}
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
        title={
          drawerMode === "create"
            ? "新增供应商"
            : drawerMode === "edit"
              ? "编辑供应商"
              : drawerItem?.name
        }
        footer={
          isForm ? (
            <>
              <Button variant="outline" size="sm" onClick={closeDrawer}>
                取消
              </Button>
              <Button size="sm" disabled={saving} onClick={() => void handleSave()}>
                {saving ? "保存中..." : "保存"}
              </Button>
            </>
          ) : drawerItem && canEdit ? (
            <>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  void handleDelete(drawerItem)
                  closeDrawer()
                }}
              >
                删除
              </Button>
              <Button size="sm" onClick={() => openEdit(drawerItem)}>
                编辑
              </Button>
            </>
          ) : undefined
        }
      >
        {drawerMode === "view" && drawerItem ? (
          <div className="flex flex-col gap-3 text-sm" data-testid="supplier-drawer">
            {[
              { label: "编号", value: drawerItem.code },
              { label: "联系人", value: drawerItem.contact },
              { label: "电话", value: drawerItem.phone },
              { label: "邮箱", value: drawerItem.email },
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
          <div className="flex flex-col gap-4" data-testid="supplier-drawer">
            {FORM_FIELDS.map(({ key, label, required, placeholder }) => (
              <div key={key} className="flex flex-col gap-1.5">
                <Label htmlFor={`supplier-${key}`}>
                  {label}
                  {required && <span className="text-destructive"> *</span>}
                </Label>
                <Input
                  id={`supplier-${key}`}
                  value={form[key]}
                  onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
                  placeholder={placeholder}
                />
              </div>
            ))}
          </div>
        )}
      </Sheet>
      {payFor && (
        <ReceiptDialog
          open
          onOpenChange={(o) => {
            if (!o) setPayFor(null)
          }}
          direction="ap"
          partnerId={payFor.id}
          partnerName={payFor.name}
          owed={owed?.get(payFor.id)}
          onDone={() => setOwedTick((n) => n + 1)}
        />
      )}
      <CsvImportDialog
        open={showImport}
        onOpenChange={setShowImport}
        noun="供应商"
        templateFilename="供应商导入模板.csv"
        fetchTemplate={supplierImportTemplate}
        importFile={(file, idempotencyKey) => importSuppliers(file, { idempotencyKey })}
        onImported={() => load(q, 0)}
      />
    </PageContainer>
  )
}
