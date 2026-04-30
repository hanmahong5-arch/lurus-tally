"use client"

import { useState, useEffect, useRef } from "react"
import { useProfile, ProfileGate } from "@/lib/profile"
import { UnitSelector } from "@/components/unit-selector"
import { HsCodeInput } from "@/components/cross-border/hs-code-input"
import type {
  CreateProductInput,
  MeasurementStrategy,
  Product,
} from "@/lib/api/products"

type ProfileType = "cross_border" | "retail" | "hybrid" | null

interface ProductFormProps {
  /** When provided, the form renders in edit mode pre-populated with this product */
  initial?: Partial<Product>
  /** Called with the serialised form values when the user submits */
  onSubmit: (input: CreateProductInput) => Promise<void>
  /** Called when the user cancels */
  onCancel?: () => void
  /** Tenant ID forwarded to UnitSelector for API calls (dev only) */
  tenantId?: string
  disabled?: boolean
  /** Optional callback fired on every field change with the current draft state. */
  onChange?: (draft: Partial<CreateProductInput>) => void
}

const STRATEGY_LABELS: Record<MeasurementStrategy, string> = {
  individual: "标准件",
  weight: "按重量",
  length: "按长度",
  volume: "按体积",
  batch: "批次管理",
  serial: "序列号管理",
}

/**
 * ProductForm is a profile-aware form for creating and editing products.
 *
 * Profile-aware fields:
 *   cross_border: hs_code (attributes.hs_code), en_name (attributes.en_name),
 *                 origin (attributes.origin)
 *   retail:       is_bulk (attributes.is_bulk), allow_credit (attributes.allow_credit)
 *
 * Profile is read from useProfile() which is a stub returning 'cross_border' until
 * Story 2.1 implements real session integration.
 * Story 2.1 TODO: wrap this form inside ProfileProvider with the real profileType.
 */
export function ProductForm({
  initial,
  onSubmit,
  onCancel,
  tenantId,
  disabled,
  onChange,
}: ProductFormProps) {
  const { profileType } = useProfile()

  const [code, setCode] = useState(initial?.code ?? "")
  const [name, setName] = useState(initial?.name ?? "")
  const [manufacturer, setManufacturer] = useState(initial?.manufacturer ?? "")
  const [model, setModel] = useState(initial?.model ?? "")
  const [spec, setSpec] = useState(initial?.spec ?? "")
  const [brand, setBrand] = useState(initial?.brand ?? "")
  const [remark, setRemark] = useState(initial?.remark ?? "")
  const [measurementStrategy, setMeasurementStrategy] =
    useState<MeasurementStrategy>(
      (initial?.measurement_strategy as MeasurementStrategy) ?? "individual"
    )
  const [defaultUnitId, setDefaultUnitId] = useState<string | undefined>(
    initial?.default_unit_id
  )

  // Cross-border profile attributes
  const initialAttrs = (initial?.attributes ?? {}) as Record<string, unknown>
  const [hsCode, setHsCode] = useState<string>(
    String(initialAttrs.hs_code ?? "")
  )
  const [enName, setEnName] = useState<string>(
    String(initialAttrs.en_name ?? "")
  )
  const [origin, setOrigin] = useState<string>(
    String(initialAttrs.origin ?? "")
  )

  // Retail profile attributes
  const [isBulk, setIsBulk] = useState<boolean>(
    Boolean(initialAttrs.is_bulk ?? false)
  )
  const [allowCredit, setAllowCredit] = useState<boolean>(
    Boolean(initialAttrs.allow_credit ?? false)
  )

  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Ref to skip the synchronous mount effect (fire onChange only on user edits).
  const isFirstRenderRef = useRef(true)

  // Notify parent of field changes for draft persistence (skips initial mount).
  useEffect(() => {
    if (isFirstRenderRef.current) {
      isFirstRenderRef.current = false
      return
    }
    if (!onChange) return
    onChange({
      code,
      name,
      manufacturer: manufacturer || undefined,
      model: model || undefined,
      spec: spec || undefined,
      brand: brand || undefined,
      remark: remark || undefined,
      measurement_strategy: measurementStrategy,
      default_unit_id: defaultUnitId,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [code, name, manufacturer, model, spec, brand, remark, measurementStrategy, defaultUnitId])

  function buildAttributes(profile: ProfileType): Record<string, unknown> {
    if (profile === "cross_border") {
      const attrs: Record<string, unknown> = {}
      if (hsCode) attrs.hs_code = hsCode
      if (enName) attrs.en_name = enName
      if (origin) attrs.origin = origin
      return attrs
    }
    if (profile === "retail") {
      return { is_bulk: isBulk, allow_credit: allowCredit }
    }
    return {}
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await onSubmit({
        code,
        name,
        manufacturer: manufacturer || undefined,
        model: model || undefined,
        spec: spec || undefined,
        brand: brand || undefined,
        remark: remark || undefined,
        measurement_strategy: measurementStrategy,
        default_unit_id: defaultUnitId,
        attributes: buildAttributes(profileType),
      })
    } catch (e) {
      setError(String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const inputCls =
    "w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
  const labelCls = "block text-sm font-medium text-foreground mb-1"

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/30 px-3 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className={labelCls}>
            商品编码 <span className="text-destructive">*</span>
          </label>
          <input
            className={inputCls}
            value={code}
            onChange={(e) => setCode(e.target.value)}
            required
            disabled={disabled || submitting}
            placeholder="如 SKU-001"
          />
        </div>
        <div>
          <label className={labelCls}>
            商品名称 <span className="text-destructive">*</span>
          </label>
          <input
            className={inputCls}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            disabled={disabled || submitting}
            placeholder="商品全称"
          />
        </div>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <div>
          <label className={labelCls}>品牌</label>
          <input
            className={inputCls}
            value={brand}
            onChange={(e) => setBrand(e.target.value)}
            disabled={disabled || submitting}
          />
        </div>
        <div>
          <label className={labelCls}>型号</label>
          <input
            className={inputCls}
            value={model}
            onChange={(e) => setModel(e.target.value)}
            disabled={disabled || submitting}
          />
        </div>
        <div>
          <label className={labelCls}>规格</label>
          <input
            className={inputCls}
            value={spec}
            onChange={(e) => setSpec(e.target.value)}
            disabled={disabled || submitting}
          />
        </div>
      </div>

      <div>
        <label className={labelCls}>制造商</label>
        <input
          className={inputCls}
          value={manufacturer}
          onChange={(e) => setManufacturer(e.target.value)}
          disabled={disabled || submitting}
        />
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className={labelCls}>计量策略</label>
          <select
            className={inputCls}
            value={measurementStrategy}
            onChange={(e) =>
              setMeasurementStrategy(e.target.value as MeasurementStrategy)
            }
            disabled={disabled || submitting}
          >
            {Object.entries(STRATEGY_LABELS).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className={labelCls}>默认单位</label>
          <UnitSelector
            value={defaultUnitId}
            onChange={setDefaultUnitId}
            tenantId={tenantId}
            disabled={disabled || submitting}
            className={inputCls}
          />
        </div>
      </div>

      {/* cross_border profile fields */}
      <ProfileGate profiles={["cross_border"]}>
        <div className="rounded-lg border border-border p-4 space-y-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            跨境贸易信息
          </p>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className={labelCls}>HS Code</label>
              <HsCodeInput
                className={inputCls}
                value={hsCode}
                onChange={setHsCode}
                disabled={disabled || submitting}
              />
            </div>
            <div>
              <label className={labelCls}>英文名称</label>
              <input
                className={inputCls}
                value={enName}
                onChange={(e) => setEnName(e.target.value)}
                disabled={disabled || submitting}
                placeholder="English name"
              />
            </div>
            <div>
              <label className={labelCls}>原产地</label>
              <input
                className={inputCls}
                value={origin}
                onChange={(e) => setOrigin(e.target.value)}
                disabled={disabled || submitting}
                placeholder="如 China"
              />
            </div>
          </div>
        </div>
      </ProfileGate>

      {/* retail profile fields */}
      <ProfileGate profiles={["retail"]}>
        <div className="rounded-lg border border-border p-4 space-y-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            零售设置
          </p>
          <div className="flex gap-6">
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="checkbox"
                checked={isBulk}
                onChange={(e) => setIsBulk(e.target.checked)}
                disabled={disabled || submitting}
                className="rounded"
              />
              散装/称重商品
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="checkbox"
                checked={allowCredit}
                onChange={(e) => setAllowCredit(e.target.checked)}
                disabled={disabled || submitting}
                className="rounded"
              />
              允许赊账
            </label>
          </div>
        </div>
      </ProfileGate>

      <div>
        <label className={labelCls}>备注</label>
        <textarea
          className={inputCls + " min-h-[80px] resize-y"}
          value={remark}
          onChange={(e) => setRemark(e.target.value)}
          disabled={disabled || submitting}
        />
      </div>

      <div className="flex justify-end gap-3 pt-2">
        {onCancel && (
          <button
            type="button"
            onClick={onCancel}
            disabled={submitting}
            className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted transition-colors disabled:opacity-50"
          >
            取消
          </button>
        )}
        <button
          type="submit"
          disabled={disabled || submitting}
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
        >
          {submitting ? "保存中..." : "保存"}
        </button>
      </div>
    </form>
  )
}
