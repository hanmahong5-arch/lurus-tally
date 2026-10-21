"use client"

/**
 * 开单明细行的公共字段：数量 / 单价 / 小计 / 删除按钮 / 上次价提示。
 * 采购与销售两个编辑器共用，保证手机（卡片）与桌面（表格）两种版式的输入行为一致。
 * 数字输入用 inputMode="decimal" 唤起带小数点的数字键盘；手机端字号 16px（text-base），
 * 避免 iOS 聚焦时页面自动放大，桌面端回落到 text-sm。
 */

import type { ReactNode } from "react"
import { formatLastPriceHint } from "@/lib/api/last-prices"
import type { LastPriceLine } from "@/hooks/use-last-price-fill"

export const lineInputClass =
  "w-full rounded-md border border-input bg-transparent px-2 py-1 text-base md:text-sm text-right outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

interface NumberFieldProps {
  value: string
  min: string
  step: string
  readOnly?: boolean
  onChange: (v: string) => void
  ariaLabel?: string
}

/** 数量 / 单价输入框。 */
export function LineNumberInput({ value, min, step, readOnly, onChange, ariaLabel }: NumberFieldProps) {
  return (
    <input
      type="number"
      inputMode="decimal"
      aria-label={ariaLabel}
      className={lineInputClass}
      value={value}
      min={min}
      step={step}
      readOnly={readOnly}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}

/** 上次价 / 默认价提示。 */
export function LastPriceHint({ last }: { last?: LastPriceLine["_last_price"] }) {
  if (!last) return null
  return (
    <p className="mt-0.5 text-right text-xs text-muted-foreground">
      {formatLastPriceHint(last.price, last.date, {
        source: last.source,
        unitName: last.unit_name,
        basePrice: last.base_price,
      })}
    </p>
  )
}

/** 手机端行内的运算符（× / =），桌面表格里不显示。 */
export function MobileOp({ children }: { children: ReactNode }) {
  return (
    <span aria-hidden="true" className="mr-1 text-muted-foreground md:hidden">
      {children}
    </span>
  )
}

/** 删除行按钮。 */
export function RemoveLineButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label="删除此行"
      className="inline-flex min-h-[36px] min-w-[36px] items-center justify-center text-destructive hover:text-destructive/70 text-sm md:text-xs"
      onClick={onClick}
    >
      ✕
    </button>
  )
}
