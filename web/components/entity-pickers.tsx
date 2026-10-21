"use client"

import { useCallback } from "react"
import { toast } from "sonner"
import { BarcodeScanner } from "@/components/barcode-scanner"
import { EntityCombobox, type EntityOption } from "@/components/entity-combobox"
import { useTenantId } from "@/hooks/use-tenant-id"
import { createProduct, listProducts } from "@/lib/api/products"
import { createSupplier, listSuppliers } from "@/lib/api/suppliers"
import { ApiError } from "@/lib/api/errors"
import { createCustomer, customerDuplicateOf, getCustomer, listCustomers } from "@/lib/api/customers"

interface PickerProps {
  value: string
  label: string
  onSelect: (option: EntityOption) => void
  onClear?: () => void
  disabled?: boolean
  placeholder?: string
  id?: string
  className?: string
}

const normalizeName = (s: string) => s.trim().replace(/\s+/g, " ").toLowerCase()

/**
 * Bill-form pickers. Each searches its own catalogue and, when the typed name
 * is not there, creates it in that catalogue — so a product added on a
 * purchase line, or a supplier / customer added on a bill header, shows up in
 * the 商品 / 供应商 / 客户 lists afterwards.
 */
export function ProductPicker(props: PickerProps) {
  const tenantId = useTenantId()
  const search = useCallback(
    async (q: string, signal: AbortSignal) => {
      const res = await listProducts({ q: q || undefined, limit: 20, enabled: true, tenantId, signal })
      return res.items.map((p) => ({ id: p.id, name: p.name, sub: [p.code, p.spec].filter(Boolean).join(" · ") }))
    },
    [tenantId]
  )
  const create = useCallback(
    async (name: string) => {
      const p = await createProduct({ name }, tenantId)
      return { id: p.id, name: p.name, sub: p.code }
    },
    [tenantId]
  )
  // 扫码:按条码精确查(后端 q 含 barcode 精确匹配),唯一命中即选中。
  const onScan = useCallback(
    async (code: string) => {
      try {
        const res = await listProducts({ q: code, limit: 2, enabled: true, tenantId })
        if (res.items.length === 1) {
          const p = res.items[0]
          props.onSelect({ id: p.id, name: p.name, sub: [p.code, p.spec].filter(Boolean).join(" · ") })
        } else {
          toast.info(res.items.length === 0 ? `未找到条码「${code}」对应的商品` : `条码「${code}」对应多个商品，请手动搜索选择`)
        }
      } catch {
        toast.error("扫码查询失败，请重试")
      }
    },
    [tenantId, props]
  )
  return (
    <div className="flex items-center gap-1">
      <EntityCombobox {...props} className={props.className ?? "flex-1"} search={search} create={create} entityLabel="商品" />
      {!props.disabled && <BarcodeScanner onScan={onScan} />}
    </div>
  )
}

export function SupplierPicker(props: PickerProps) {
  const tenantId = useTenantId()
  const search = useCallback(
    async (q: string, signal: AbortSignal) => {
      const res = await listSuppliers({ q: q || undefined, limit: 20, tenantId, signal })
      return res.items.map((s) => ({ id: s.id, name: s.name, sub: s.phone || s.code || undefined }))
    },
    [tenantId]
  )
  const create = useCallback(
    async (name: string) => {
      try {
        const s = await createSupplier({ name }, tenantId)
        return { id: s.id, name: s.name }
      } catch (e) {
        if (!(e instanceof ApiError) || e.status !== 409) throw e
        // The 409 body carries no id: find the same-name supplier and use it.
        const key = normalizeName(name)
        const res = await listSuppliers({ q: name, limit: 20, tenantId })
        const hit = res.items.find((x) => normalizeName(x.name) === key)
        if (!hit) throw new Error("已存在同名供应商，请在列表中搜索后选择")
        toast.info("已存在同名供应商，已为你选中")
        return { id: hit.id, name: hit.name, reused: true }
      }
    },
    [tenantId]
  )
  return <EntityCombobox {...props} search={search} create={create} entityLabel="供应商" />
}

export function CustomerPicker(props: PickerProps) {
  const tenantId = useTenantId()
  const search = useCallback(
    async (q: string, signal: AbortSignal) => {
      const res = await listCustomers({ q: q || undefined, limit: 20, tenantId, signal })
      return res.items.map((c) => ({ id: c.id, name: c.name, sub: c.mobile || c.phone || c.code || undefined }))
    },
    [tenantId]
  )
  const create = useCallback(
    async (name: string) => {
      try {
        const c = await createCustomer({ name }, tenantId)
        return { id: c.id, name: c.name }
      } catch (e) {
        const dup = customerDuplicateOf(e)
        if (!dup) throw e
        const existing = await getCustomer(dup.existingId, tenantId).catch(() => null)
        toast.info("已存在同名客户，已为你选中")
        return { id: dup.existingId, name: existing?.name ?? name, reused: true }
      }
    },
    [tenantId]
  )
  return <EntityCombobox {...props} search={search} create={create} entityLabel="客户" />
}
