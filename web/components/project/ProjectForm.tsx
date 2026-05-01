"use client"

import { useState } from "react"
import {
  createProject,
  updateProject,
  type ProjectItem,
  type ProjectStatus,
} from "@/lib/api/projects"

const STATUS_OPTIONS: { value: ProjectStatus; label: string }[] = [
  { value: "active", label: "进行中" },
  { value: "paused", label: "已暂停" },
  { value: "completed", label: "已完工" },
  { value: "cancelled", label: "已取消" },
]

interface Props {
  mode: "create" | "edit"
  initialData?: ProjectItem
  onSuccess: (item: ProjectItem) => void
  onCancel: () => void
}

/**
 * ProjectForm — create or edit a project entry (Story 28.2).
 * Customer field is a plain text input for MVP; Combobox deferred to S28.8.
 */
export default function ProjectForm({
  mode,
  initialData,
  onSuccess,
  onCancel,
}: Props) {
  const [code, setCode] = useState(initialData?.code ?? "")
  const [name, setName] = useState(initialData?.name ?? "")
  const [customerId, setCustomerId] = useState(initialData?.customerId ?? "")
  const [contractAmount, setContractAmount] = useState(
    initialData?.contractAmount ?? ""
  )
  const [startDate, setStartDate] = useState(initialData?.startDate ?? "")
  const [endDate, setEndDate] = useState(initialData?.endDate ?? "")
  const [status, setStatus] = useState<ProjectStatus>(
    initialData?.status ?? "active"
  )
  const [address, setAddress] = useState(initialData?.address ?? "")
  const [manager, setManager] = useState(initialData?.manager ?? "")
  const [remark, setRemark] = useState(initialData?.remark ?? "")

  const [codeError, setCodeError] = useState("")
  const [nameError, setNameError] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState("")

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setCodeError("")
    setNameError("")
    setSubmitError("")

    let valid = true
    if (!name.trim()) {
      setNameError("项目名称不能为空")
      valid = false
    }
    if (!code.trim()) {
      setCodeError("项目编号不能为空")
      valid = false
    }
    if (!valid) return

    setSubmitting(true)
    try {
      const payload = {
        code: code.trim(),
        name: name.trim(),
        customerId: customerId.trim() || undefined,
        contractAmount: contractAmount.trim() || undefined,
        startDate: startDate || undefined,
        endDate: endDate || undefined,
        status,
        address: address.trim(),
        manager: manager.trim(),
        remark: remark.trim(),
      }

      let item: ProjectItem
      if (mode === "create") {
        item = await createProject(payload)
      } else {
        item = await updateProject(initialData!.id, payload)
      }
      onSuccess(item)
    } catch (e) {
      setSubmitError(String(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        void handleSubmit(e)
      }}
      className="flex flex-col gap-4 text-sm"
    >
      {/* Project code */}
      <div>
        <label className="block text-xs font-medium mb-1">
          项目编号 <span className="text-destructive">*</span>
        </label>
        <input
          name="code"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder="请输入项目编号"
        />
        {codeError && (
          <p className="mt-1 text-xs text-destructive">{codeError}</p>
        )}
      </div>

      {/* Project name */}
      <div>
        <label className="block text-xs font-medium mb-1">
          项目名称 <span className="text-destructive">*</span>
        </label>
        <input
          name="name"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="请输入项目名称"
        />
        {nameError && (
          <p className="mt-1 text-xs text-destructive">{nameError}</p>
        )}
      </div>

      {/* Customer field — plain text input for MVP */}
      {/* TODO(S28.8): swap to partner Combobox once partner search API is ready */}
      <div data-testid="customer-field">
        <label className="block text-xs font-medium mb-1">客户ID</label>
        <input
          name="customerId"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={customerId}
          onChange={(e) => setCustomerId(e.target.value)}
          placeholder="客户 UUID（可选）"
        />
      </div>

      {/* Contract amount */}
      <div>
        <label className="block text-xs font-medium mb-1">合同金额</label>
        <input
          name="contractAmount"
          type="text"
          inputMode="decimal"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={contractAmount}
          onChange={(e) => setContractAmount(e.target.value)}
          placeholder="0.00"
        />
      </div>

      {/* Date range */}
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium mb-1">开工日期</label>
          <input
            name="startDate"
            type="date"
            className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
            value={startDate}
            onChange={(e) => setStartDate(e.target.value)}
          />
        </div>
        <div>
          <label className="block text-xs font-medium mb-1">完工日期</label>
          <input
            name="endDate"
            type="date"
            className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
            value={endDate}
            onChange={(e) => setEndDate(e.target.value)}
          />
        </div>
      </div>

      {/* Status */}
      <div>
        <label className="block text-xs font-medium mb-1">状态</label>
        <select
          name="status"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none"
          value={status}
          onChange={(e) => setStatus(e.target.value as ProjectStatus)}
        >
          {STATUS_OPTIONS.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </div>

      {/* Address */}
      <div>
        <label className="block text-xs font-medium mb-1">地址</label>
        <input
          name="address"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={address}
          onChange={(e) => setAddress(e.target.value)}
          placeholder="项目地址"
        />
      </div>

      {/* Manager */}
      <div>
        <label className="block text-xs font-medium mb-1">项目负责人</label>
        <input
          name="manager"
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          value={manager}
          onChange={(e) => setManager(e.target.value)}
          placeholder="负责人姓名"
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
