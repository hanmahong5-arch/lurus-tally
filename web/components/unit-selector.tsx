"use client"

import { useEffect, useState } from "react"
import { createUnit, listUnits, type UnitDef, type UnitType } from "@/lib/api/units"
import { extractApiError } from "@/lib/api/errors"
import { useProfile } from "@/lib/profile"

const NEW_UNIT = "__new__"

// Units a nursery rings up in, in the order shown first.
const NURSERY_UNIT_ORDER = ["株", "棵", "丛", "袋", "平方米"]

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
 * and renders a native <select> element. The last option lets the user add a
 * unit on the spot (name only; the backend generates the code).
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
  const [adding, setAdding] = useState(false)
  const [newName, setNewName] = useState("")
  const [newType, setNewType] = useState<UnitType>(unitType ?? "count")
  const [saving, setSaving] = useState(false)
  const [addError, setAddError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const { profileType } = useProfile()

  useEffect(() => {
    setLoading(true)
    setError(null)
    listUnits(unitType, tenantId)
      .then((res) => setUnits(res.items ?? []))
      .catch(() => setError("单位没加载出来，请刷新重试"))
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
        <option>{error}</option>
      </select>
    )
  }

  async function handleAdd() {
    const name = newName.trim()
    if (!name) {
      setAddError("请填写单位名称")
      return
    }
    // The same name again would only be refused (or double up): just pick the one that exists.
    const existing = units.find((u) => sameUnitName(u.name, name))
    if (existing) {
      onChange(existing.id)
      setAdding(false)
      setNewName("")
      setAddError(null)
      setNotice("已有这个单位，已帮你选上")
      return
    }
    setSaving(true)
    setAddError(null)
    try {
      const created = await createUnit({ name, unit_type: unitType ?? newType }, tenantId)
      setUnits((prev) => [...prev, created])
      onChange(created.id)
      setNotice(null)
      setAdding(false)
      setNewName("")
      setNewType("count")
    } catch (e) {
      setAddError(extractApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  function cancelAdd() {
    setAdding(false)
    setAddError(null)
  }

  // Shop-made units (e.g. 株, 盆) come first; the built-in ones follow. A nursery sees its own
  // units (株, 棵, 丛, 袋, 平方米) first, in that order.
  const nurseryRank = (name: string) => {
    const i = NURSERY_UNIT_ORDER.indexOf(name)
    return i < 0 ? NURSERY_UNIT_ORDER.length : i
  }
  const sortedUnits = [...units].sort((a, b) => {
    if (profileType === "horticulture") {
      const d = nurseryRank(a.name) - nurseryRank(b.name)
      if (d !== 0) return d
    }
    return Number(a.is_system) - Number(b.is_system)
  })

  const select = (
    <select
      value={value ?? ""}
      onChange={(e) => {
        setNotice(null)
        if (e.target.value === NEW_UNIT) {
          setAdding(true)
          return
        }
        onChange(e.target.value || undefined)
      }}
      className={className}
      disabled={disabled}
    >
      <option value="">{placeholder}</option>
      {sortedUnits.map((u) => (
        <option key={u.id} value={u.id}>
          {u.name}
        </option>
      ))}
      <option value={NEW_UNIT}>+ 新建单位</option>
    </select>
  )

  if (!adding) {
    return (
      <>
        {select}
        {notice && <p className="mt-1 text-xs text-muted-foreground">{notice}</p>}
      </>
    )
  }

  return (
    <>
      {select}
      <div className="mt-1 flex items-center gap-2">
        <input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          placeholder="单位名称，如：盆"
          maxLength={20}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              // Inside the product form: Enter must add the unit, not submit the form.
              e.preventDefault()
              handleAdd()
            } else if (e.key === "Escape") {
              e.preventDefault()
              cancelAdd()
            }
          }}
          className="h-8 w-32 rounded border px-2 text-sm"
        />
        {!unitType && (
          <select
            value={newType}
            onChange={(e) => setNewType(e.target.value as UnitType)}
            aria-label="计量方式"
            className="h-8 rounded border px-1 text-sm"
          >
            <option value="count">按个数</option>
            <option value="area">按面积</option>
            <option value="weight">按重量</option>
            <option value="length">按长度</option>
          </select>
        )}
        <button type="button" onClick={handleAdd} disabled={saving} className="text-sm text-primary">
          {saving ? "添加中..." : "添加"}
        </button>
        <button
          type="button"
          onClick={cancelAdd}
          className="text-sm text-muted-foreground"
        >
          取消
        </button>
      </div>
      {addError && <p className="mt-1 text-xs text-destructive">{addError}</p>}
    </>
  )
}

/** Same unit if only case or full/half-width forms differ (Ｍ vs m, ＫＧ vs kg). */
function sameUnitName(a: string, b: string): boolean {
  const norm = (s: string) => s.normalize("NFKC").trim().toLowerCase()
  return norm(a) === norm(b)
}
