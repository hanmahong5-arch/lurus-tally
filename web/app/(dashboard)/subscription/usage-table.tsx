"use client"

import { useEffect, useState } from "react"

import {
  type PlanLimit,
  type PlanLimitKey,
  getSubscriptionLockState,
} from "@/lib/api/billing"

const ROWS: { key: PlanLimitKey; label: string; unit: string }[] = [
  { key: "max_users", label: "成员", unit: "人" },
  { key: "max_skus", label: "商品", unit: "个" },
  { key: "max_warehouses", label: "仓库", unit: "个" },
]

function limitText(l: PlanLimit, unit: string): string {
  return l.limit < 0 ? "不限" : `${l.limit} ${unit}`
}

/**
 * 套餐用量小表：只展示，上限 -1 显示「不限」。
 * 取不到 limits（无平台套餐、请求失败）时整块不渲染。
 */
export function UsageTable() {
  const [limits, setLimits] = useState<Partial<Record<PlanLimitKey, PlanLimit>> | null>(null)

  useEffect(() => {
    let cancelled = false
    getSubscriptionLockState()
      .then((st) => {
        if (!cancelled) setLimits(st.limits ?? null)
      })
      .catch(() => {
        if (!cancelled) setLimits(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (!limits) return null

  return (
    <section aria-label="套餐用量" className="mt-6">
      <h2 className="mb-2 text-sm font-medium text-muted-foreground">用量</h2>
      <table className="w-full max-w-md border-collapse text-sm">
        <thead>
          <tr className="border-b text-left text-muted-foreground">
            <th className="py-2 pr-4 font-normal">项目</th>
            <th className="py-2 pr-4 font-normal">已用</th>
            <th className="py-2 font-normal">套餐上限</th>
          </tr>
        </thead>
        <tbody>
          {ROWS.map(({ key, label, unit }) => {
            const l = limits[key]
            if (!l) return null
            return (
              <tr key={key} className="border-b last:border-0">
                <td className="py-2 pr-4">{label}</td>
                <td className="py-2 pr-4 tabular-nums">
                  {l.used} {unit}
                </td>
                <td className="py-2 tabular-nums">{limitText(l, unit)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </section>
  )
}
