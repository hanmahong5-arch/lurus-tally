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
import { ErrorBanner } from "@/components/ui/error-banner"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"

type ProfileType = "cross_border" | "retail" | "hybrid" | "horticulture" | null

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

// Shared control style for child selectors that take a className (UnitSelector,
// HsCodeInput, native <select>) — mirrors the Input primitive.
const CONTROL_CLASS =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"

/**
 * ProductForm is a profile-aware form for creating and editing products.
 *
 * Profile-aware fields:
 *   cross_border: hs_code, en_name, origin (under attributes)
 *   retail:       is_bulk, allow_credit (under attributes)
 *
 * Field state stays on useState because the page-level draft autosave hooks into
 * `onChange` on every keystroke; that integration is preserved verbatim.
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
  const [hsCode, setHsCode] = useState<string>(String(initialAttrs.hs_code ?? ""))
  const [enName, setEnName] = useState<string>(String(initialAttrs.en_name ?? ""))
  const [origin, setOrigin] = useState<string>(String(initialAttrs.origin ?? ""))

  // Retail profile attributes
  const [isBulk, setIsBulk] = useState<boolean>(Boolean(initialAttrs.is_bulk ?? false))
  const [allowCredit, setAllowCredit] = useState<boolean>(Boolean(initialAttrs.allow_credit ?? false))

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

  const busy = disabled || submitting

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {error && <ErrorBanner>{error}</ErrorBanner>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="pf-code">
            商品编码 <span className="text-destructive">*</span>
          </Label>
          <Input
            id="pf-code"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            required
            disabled={busy}
            placeholder="如 SKU-001"
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="pf-name">
            商品名称 <span className="text-destructive">*</span>
          </Label>
          <Input
            id="pf-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            disabled={busy}
            placeholder="商品全称"
          />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div className="space-y-1.5">
          <Label htmlFor="pf-brand">品牌</Label>
          <Input id="pf-brand" value={brand} onChange={(e) => setBrand(e.target.value)} disabled={busy} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="pf-model">型号</Label>
          <Input id="pf-model" value={model} onChange={(e) => setModel(e.target.value)} disabled={busy} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="pf-spec">规格</Label>
          <Input id="pf-spec" value={spec} onChange={(e) => setSpec(e.target.value)} disabled={busy} />
        </div>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="pf-manufacturer">制造商</Label>
        <Input
          id="pf-manufacturer"
          value={manufacturer}
          onChange={(e) => setManufacturer(e.target.value)}
          disabled={busy}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="pf-strategy">计量策略</Label>
          <select
            id="pf-strategy"
            className={CONTROL_CLASS}
            value={measurementStrategy}
            onChange={(e) => setMeasurementStrategy(e.target.value as MeasurementStrategy)}
            disabled={busy}
          >
            {Object.entries(STRATEGY_LABELS).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>
        <div className="space-y-1.5">
          <Label>默认单位</Label>
          <UnitSelector
            value={defaultUnitId}
            onChange={setDefaultUnitId}
            tenantId={tenantId}
            disabled={busy}
            className={CONTROL_CLASS}
          />
        </div>
      </div>

      {/* cross_border profile fields */}
      <ProfileGate profiles={["cross_border"]}>
        <div className="space-y-3 rounded-lg border border-border p-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            跨境贸易信息
          </p>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label>HS Code</Label>
              <HsCodeInput className={CONTROL_CLASS} value={hsCode} onChange={setHsCode} disabled={busy} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pf-enname">英文名称</Label>
              <Input
                id="pf-enname"
                value={enName}
                onChange={(e) => setEnName(e.target.value)}
                disabled={busy}
                placeholder="English name"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pf-origin">原产地</Label>
              <Input
                id="pf-origin"
                value={origin}
                onChange={(e) => setOrigin(e.target.value)}
                disabled={busy}
                placeholder="如 China"
              />
            </div>
          </div>
        </div>
      </ProfileGate>

      {/* retail profile fields */}
      <ProfileGate profiles={["retail"]}>
        <div className="space-y-3 rounded-lg border border-border p-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            零售设置
          </p>
          <div className="flex flex-wrap gap-6">
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <Checkbox
                checked={isBulk}
                onCheckedChange={(c) => setIsBulk(c === true)}
                disabled={busy}
              />
              散装/称重商品
            </label>
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <Checkbox
                checked={allowCredit}
                onCheckedChange={(c) => setAllowCredit(c === true)}
                disabled={busy}
              />
              允许赊账
            </label>
          </div>
        </div>
      </ProfileGate>

      <div className="space-y-1.5">
        <Label htmlFor="pf-remark">备注</Label>
        <Textarea
          id="pf-remark"
          className="min-h-[80px] resize-y"
          value={remark}
          onChange={(e) => setRemark(e.target.value)}
          disabled={busy}
        />
      </div>

      <div className="flex justify-end gap-3 pt-2">
        {onCancel && (
          <Button type="button" variant="outline" onClick={onCancel} disabled={submitting}>
            取消
          </Button>
        )}
        <Button type="submit" disabled={busy}>
          {submitting ? "保存中..." : "保存"}
        </Button>
      </div>
    </form>
  )
}
