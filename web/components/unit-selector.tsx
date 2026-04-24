"use client"

import { useEffect, useState } from "react"
import { listUnits, type UnitDef, type UnitType } from "@/lib/api/units"

interface UnitSelectorProps {
  /** Currently selected unit ID */
  value?: string
  /** Called with the new unit ID when selection changes */
  onChange: (unitId: string | undefined) => void
  /** Optional filter to only show units of a given type */
  unitType?: UnitType
  /** Tenant ID forwarded to the API (dev only; Story 2.1 replaces with session cookie) */
  tenantId?: string
  className?: string
  disabled?: boolean
  placeholder?: string
}

/**
 * UnitSelector fetches visible unit_defs (system + tenant-custom) from /api/v1/units
 * and renders a native <select> element.
 */
export function UnitSelector({
  value,
  onChange,
  unitType,
  tenantId,
  className,
  disabled,
  placeholder = "选择单位",
}: UnitSelectorProps) {
  const [units, setUnits] = useState<UnitDef[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    listUnits(unitType, tenantId)
      .then((res) => setUnits(res.items ?? []))
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [unitType, tenantId])

  if (loading) {
    return (
      <select disabled className={className}>
        <option>加载中...</option>
      </select>
    )
  }

  if (error) {
    return (
      <select disabled className={className}>
        <option>加载失败: {error}</option>
      </select>
    )
  }

  return (
    <select
      value={value ?? ""}
      onChange={(e) => onChange(e.target.value || undefined)}
      className={className}
      disabled={disabled}
    >
      <option value="">{placeholder}</option>
      {units.map((u) => (
        <option key={u.id} value={u.id}>
          {u.name} ({u.code}){u.is_system ? " · 系统" : ""}
        </option>
      ))}
    </select>
  )
}
