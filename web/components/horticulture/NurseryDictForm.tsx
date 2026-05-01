"use client"

import { useState } from "react"
import {
  createNurseryDict,
  updateNurseryDict,
  type NurseryDictItem,
  type NurseryType,
} from "@/lib/api/nursery-dict"

const ALL_TYPES: { value: NurseryType; label: string }[] = [
  { value: "tree", label: "乔木" },
  { value: "shrub", label: "灌木" },
  { value: "herb", label: "地被/草本" },
  { value: "vine", label: "藤本" },
  { value: "bamboo", label: "竹类" },
  { value: "aquatic", label: "水生" },
  { value: "bulb", label: "球根" },
  { value: "fruit", label: "果树" },
]

const MONTHS = Array.from({ length: 12 }, (_, i) => i + 1)

interface SpecEntry {
  key: string
}

interface Props {
  mode: "create" | "edit"
  initialData?: NurseryDictItem
  onSuccess: (item: NurseryDictItem) => void
  onCancel: () => void
}

/**
 * NurseryDictForm — create or edit a nursery species entry (Story 28.1).
 * Supports dynamic spec_template key-value editor.
 */
export default function NurseryDictForm({
  mode,
  initialData,
  onSuccess,
  onCancel,
}: Props) {
  const [name, setName] = useState(initialData?.name ?? "")
  const [latinName, setLatinName] = useState(initialData?.latin_name ?? "")
  const [family, setFamily] = useState(initialData?.family ?? "")
  const [genus, setGenus] = useState(initialData?.genus ?? "")
  const [type, setType] = useState<NurseryType>(initialData?.type ?? "tree")
  const [isEvergreen, setIsEvergreen] = useState(initialData?.is_evergreen ?? false)
  const [climateZones, setClimateZones] = useState(
    initialData?.climate_zones.join(",") ?? ""
  )
  const [seasonStart, setSeasonStart] = useState(
    initialData?.best_season[0] ?? 0
  )
  const [seasonEnd, setSeasonEnd] = useState(
    initialData?.best_season[1] ?? 0
  )
  const [specEntries, setSpecEntries] = useState<SpecEntry[]>(
    initialData?.spec_template
      ? Object.keys(initialData.spec_template).map((k) => ({ key: k }))
      : []
  )
  const [photoUrl, setPhotoUrl] = useState(initialData?.photo_url ?? "")
  const [remark, setRemark] = useState(initialData?.remark ?? "")

  const [nameError, setNameError] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState("")

  function addSpecEntry() {
    setSpecEntries((prev) => [...prev, { key: "" }])
  }

  function updateSpecKey(index: number, key: string) {
    setSpecEntries((prev) => {
      const next = Array.from(prev)
      next[index] = { key }
      return next
    })
  }

  function removeSpecEntry(index: number) {
    setSpecEntries((prev) => prev.filter((_, i) => i !== index))
  }

  function buildSpecTemplate(): Record<string, null> {
    const result: Record<string, null> = {}
    for (const entry of specEntries) {
      if (entry.key.trim()) {
        result[entry.key.trim()] = null
      }
    }
    return result
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setNameError("")
    setSubmitError("")

    if (!name.trim()) {
      setNameError("名称不能为空")
      return
    }

    setSubmitting(true)
    try {
      const payload = {
        name: name.trim(),
        latin_name: latinName.trim(),
        family: family.trim(),
        genus: genus.trim(),
        type,
        is_evergreen: isEvergreen,
        climate_zones: climateZones
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        best_season: [seasonStart, seasonEnd] as [number, number],
        spec_template: buildSpecTemplate(),
        photo_url: photoUrl.trim(),
        remark: remark.trim(),
      }

      let item: NurseryDictItem
      if (mode === "create") {
        item = await createNurseryDict(payload)
      } else {
        item = await updateNurseryDict(initialData!.id, payload)
      }
      onSuccess(item)
    } catch (e) {
      setSubmitError(String(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={(e) => { void handleSubmit(e) }} className="flex flex-col gap-4 text-sm">
      {/* Name */}
      <div>
        <label className="block text-xs font-medium mb-1">
          名称 <span className="text-destructive">*</span>
        </label>
        <input
          name="name"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="请输入苗木名称"
        />
        {nameError && (
          <p className="mt-1 text-xs text-destructive">{nameError}</p>
        )}
      </div>

      {/* Latin name */}
      <div>
        <label className="block text-xs font-medium mb-1">拉丁名</label>
        <input
          name="latin_name"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={latinName}
          onChange={(e) => setLatinName(e.target.value)}
          placeholder="e.g. Acer palmatum"
        />
      </div>

      {/* Family + Genus */}
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium mb-1">科</label>
          <input
            name="family"
            className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
            value={family}
            onChange={(e) => setFamily(e.target.value)}
            placeholder="科名"
          />
        </div>
        <div>
          <label className="block text-xs font-medium mb-1">属</label>
          <input
            name="genus"
            className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
            value={genus}
            onChange={(e) => setGenus(e.target.value)}
            placeholder="属名"
          />
        </div>
      </div>

      {/* Type */}
      <div>
        <label className="block text-xs font-medium mb-1">类型</label>
        <select
          name="type"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none"
          value={type}
          onChange={(e) => setType(e.target.value as NurseryType)}
        >
          {ALL_TYPES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
      </div>

      {/* Evergreen */}
      <div className="flex items-center gap-2">
        <input
          type="checkbox"
          id="is_evergreen"
          checked={isEvergreen}
          onChange={(e) => setIsEvergreen(e.target.checked)}
        />
        <label htmlFor="is_evergreen" className="text-sm">常绿</label>
      </div>

      {/* Climate zones */}
      <div>
        <label className="block text-xs font-medium mb-1">气候带（逗号分隔）</label>
        <input
          name="climate_zones"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={climateZones}
          onChange={(e) => setClimateZones(e.target.value)}
          placeholder="华东,华北,华中"
        />
      </div>

      {/* Best season */}
      <div>
        <label className="block text-xs font-medium mb-1">最佳移植期（月份）</label>
        <div className="flex gap-2 items-center">
          <select
            className="rounded-md border border-input bg-background px-3 py-1.5 text-sm"
            value={seasonStart}
            onChange={(e) => setSeasonStart(Number(e.target.value))}
          >
            <option value={0}>未设置</option>
            {MONTHS.map((m) => (
              <option key={m} value={m}>{m}月</option>
            ))}
          </select>
          <span>—</span>
          <select
            className="rounded-md border border-input bg-background px-3 py-1.5 text-sm"
            value={seasonEnd}
            onChange={(e) => setSeasonEnd(Number(e.target.value))}
          >
            <option value={0}>未设置</option>
            {MONTHS.map((m) => (
              <option key={m} value={m}>{m}月</option>
            ))}
          </select>
        </div>
      </div>

      {/* Spec template */}
      <div>
        <label className="block text-xs font-medium mb-1">规格模板</label>
        {specEntries.map((entry, i) => (
          <div key={i} className="flex gap-2 mb-1">
            <input
              aria-label={`spec-key-${i}`}
              className="flex-1 rounded-md border border-input bg-background px-3 py-1 text-sm outline-none"
              value={entry.key}
              onChange={(e) => updateSpecKey(i, e.target.value)}
              placeholder="规格项名称，例如 胸径_cm"
            />
            <button
              type="button"
              onClick={() => removeSpecEntry(i)}
              className="text-xs text-destructive hover:underline"
            >
              移除
            </button>
          </div>
        ))}
        <button
          type="button"
          onClick={addSpecEntry}
          className="mt-1 text-xs text-primary hover:underline"
        >
          + 添加规格项
        </button>
      </div>

      {/* Photo URL */}
      <div>
        <label className="block text-xs font-medium mb-1">图片 URL</label>
        <input
          name="photo_url"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={photoUrl}
          onChange={(e) => setPhotoUrl(e.target.value)}
          placeholder="https://..."
        />
      </div>

      {/* Remark */}
      <div>
        <label className="block text-xs font-medium mb-1">备注</label>
        <textarea
          name="remark"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          rows={2}
          value={remark}
          onChange={(e) => setRemark(e.target.value)}
          placeholder="补充说明..."
        />
      </div>

      {submitError && (
        <p className="text-xs text-destructive">{submitError}</p>
      )}

      {/* Actions */}
      <div className="flex gap-2 pt-2">
        <button
          type="submit"
          disabled={submitting}
          className="rounded-lg bg-primary px-4 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          {submitting ? "保存中..." : mode === "create" ? "创建" : "保存"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="rounded-lg border border-border px-4 py-1.5 text-sm hover:bg-muted"
        >
          取消
        </button>
      </div>
    </form>
  )
}
