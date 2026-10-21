import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export interface DataCardField {
  label: string
  value: ReactNode
}

interface DataCardProps {
  /** 标题（商品名/单号/往来单位名） */
  title: ReactNode
  /** 标题下方的小字（编码等） */
  subtitle?: ReactNode
  /** 标题行右侧：状态徽标或金额 */
  aside?: ReactNode
  /** 键值对网格 */
  fields?: DataCardField[]
  /** 底部操作区；点击不会触发整卡点击 */
  actions?: ReactNode
  className?: string
}

/** 手机端列表卡片统一骨架：标题行 + 右侧状态/金额 + 键值对网格 + 可选操作区。 */
export function DataCard({ title, subtitle, aside, fields, actions, className }: DataCardProps) {
  return (
    <div className={cn("rounded-xl border border-border bg-card p-3 text-sm", className)}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate font-medium">{title}</div>
          {subtitle ? (
            <div className="truncate text-xs text-muted-foreground">{subtitle}</div>
          ) : null}
        </div>
        {aside ? <div className="shrink-0 text-right">{aside}</div> : null}
      </div>
      {fields && fields.length > 0 ? (
        <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1.5">
          {fields.map((f) => (
            <div key={f.label} className="min-w-0">
              <dt className="text-xs text-muted-foreground">{f.label}</dt>
              <dd className="truncate">{f.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      {actions ? (
        <div
          className="mt-2 flex flex-wrap justify-end gap-2 border-t border-border pt-2"
          onClick={(e) => e.stopPropagation()}
        >
          {actions}
        </div>
      ) : null}
    </div>
  )
}
