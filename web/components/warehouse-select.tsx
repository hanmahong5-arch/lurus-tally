"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { listWarehouses, type WarehouseItem } from "@/lib/api/warehouses"

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"

// The wire field is snake_case; the shared type still declares camelCase.
type WireWarehouse = WarehouseItem & { is_default?: boolean }

const isDefaultWarehouse = (w: WireWarehouse) => Boolean(w.is_default ?? w.isDefault)

interface WarehouseSelectProps {
  id?: string
  value: string
  onChange: (id: string) => void
  tenantId?: string
  disabled?: boolean
}

/** Warehouse dropdown for bill headers: auto-picks the default (or only) warehouse. */
export function WarehouseSelect({ id, value, onChange, tenantId, disabled }: WarehouseSelectProps) {
  const [warehouses, setWarehouses] = useState<WireWarehouse[] | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    listWarehouses({ limit: 200, tenantId })
      .then((res) => {
        if (!cancelled) setWarehouses(res.items as WireWarehouse[])
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [tenantId])

  // Fill in (or repair) the selection once the list is known; also covers a
  // restored draft that carries no, or a since-deleted, warehouse.
  useEffect(() => {
    if (!warehouses || warehouses.length === 0) return
    if (value && warehouses.some((w) => w.id === value)) return
    const pick = warehouses.length === 1 ? warehouses[0] : warehouses.find(isDefaultWarehouse)
    if (pick) onChange(pick.id)
    else if (value) onChange("")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [warehouses, value])

  if (failed) {
    return <p className="text-sm text-muted-foreground">仓库加载失败，审核时将使用默认仓。</p>
  }
  if (warehouses && warehouses.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        还没有仓库，
        <Link href="/warehouses" className="text-primary hover:underline">
          先去添加一个仓库
        </Link>
      </p>
    )
  }

  return (
    <select
      id={id}
      className={SELECT_CLASS}
      value={value}
      disabled={disabled || !warehouses}
      onChange={(e) => onChange(e.target.value)}
    >
      {!warehouses && <option value="">加载中...</option>}
      {warehouses && !value && <option value="">请选择仓库</option>}
      {warehouses?.map((w) => (
        <option key={w.id} value={w.id}>
          {w.name}
          {isDefaultWarehouse(w) ? "（默认）" : ""}
        </option>
      ))}
    </select>
  )
}
