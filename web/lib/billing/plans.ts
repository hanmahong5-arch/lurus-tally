/**
 * Tally subscription plan catalog.
 *
 * Source of truth for pricing lives in lurus-platform's identity.product_plans
 * table (seeded by migration 025_seed_tally_product). This file mirrors that
 * data so the marketing copy + feature comparison can render server-side
 * without an extra round-trip.
 *
 * IMPORTANT: plan_code + billing_cycle here MUST match the seed migration.
 * Drift will cause `subscribe()` to 404 with "Plan matching ..." from platform.
 */

import type { BillingCycle } from "@/lib/api/billing"

export interface TallyPlan {
  code: string
  name: string
  cycle: BillingCycle
  priceCNY: number
  /** Highlighted in the UI for the recommended option. */
  highlight?: boolean
  /** Marketing tagline shown under the plan title. */
  tagline: string
  features: string[]
  /** Human-readable summary of the included caps. */
  caps: { skus: number; users: number; warehouses: number }
}

export const TALLY_PRODUCT_ID = "lurus-tally"

export const TALLY_PLANS: readonly TallyPlan[] = [
  {
    code: "free",
    name: "免费版",
    cycle: "forever",
    priceCNY: 0,
    tagline: "个体试用 · 单仓库 · 50 个 SKU",
    features: ["商品/库存/采购/销售基础流程", "单用户", "基础报表"],
    caps: { skus: 50, users: 1, warehouses: 1 },
  },
  {
    code: "pro",
    name: "专业版",
    cycle: "monthly",
    priceCNY: 199,
    highlight: true,
    tagline: "中小企业刚需档 · AI 助手 · 多仓库",
    features: ["AI 进货建议 / 自然语言查询", "高级报表与导出", "5 用户 · 3 仓库 · 2000 SKU"],
    caps: { skus: 2000, users: 5, warehouses: 3 },
  },
  {
    code: "pro_yearly",
    name: "专业版（年付）",
    cycle: "yearly",
    priceCNY: 1990,
    tagline: "年付立省 2 个月",
    features: ["专业版全部能力", "5 用户 · 3 仓库 · 2000 SKU", "年付返还到钱包余额"],
    caps: { skus: 2000, users: 5, warehouses: 3 },
  },
  {
    code: "enterprise",
    name: "企业版",
    cycle: "monthly",
    priceCNY: 599,
    tagline: "无上限 + 专属支持 · 多组织协同",
    features: ["不限 SKU / 用户 / 仓库", "高级报表 + 自定义看板", "1 对 1 客服 · 优先 SLA"],
    caps: { skus: -1, users: -1, warehouses: -1 },
  },
  {
    code: "enterprise_yearly",
    name: "企业版（年付）",
    cycle: "yearly",
    priceCNY: 5990,
    tagline: "企业版年付 · 再省 2 个月",
    features: ["企业版全部能力", "包含年度对账与发票"],
    caps: { skus: -1, users: -1, warehouses: -1 },
  },
] as const

/** Format an integer CNY price as "¥199" / "免费". */
export function formatPriceCNY(amount: number): string {
  if (amount === 0) return "免费"
  return `¥${amount.toLocaleString("zh-CN")}`
}

/** Localize the cycle suffix. */
export function formatCycle(cycle: BillingCycle): string {
  switch (cycle) {
    case "monthly":
      return "/ 月"
    case "yearly":
      return "/ 年"
    case "forever":
      return ""
  }
}
