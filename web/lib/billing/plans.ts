/**
 * Tally subscription plan catalog.
 *
 * 数字真源见 docs/pricing.md（建议值，待负责人裁决；平台种子需经改价迁移同步）。
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
  /** Numeric caps kept for type compatibility only: the backend enforces none of them, so no page may show them. */
  caps: { skus: number; users: number; warehouses: number }
}

/** 种子期：全部功能免费试用，不开放付费入口。正式收费时改为 false。 */
export const SEED_TRIAL = true

export const TALLY_PRODUCT_ID = "lurus-tally"

export const TALLY_PLANS: readonly TallyPlan[] = [
  {
    code: "free",
    name: "免费版",
    cycle: "forever",
    priceCNY: 0,
    tagline: "个体试用 · 基础开单记账",
    features: ["商品/库存/采购/销售基础流程"],
    caps: { skus: 300, users: 1, warehouses: 1 },
  },
  {
    code: "pro",
    name: "专业版",
    cycle: "monthly",
    priceCNY: 58,
    highlight: true,
    tagline: "中小店铺刚需档",
    features: ["免费版全部功能"],
    caps: { skus: 5000, users: 5, warehouses: 3 },
  },
  {
    code: "pro_yearly",
    name: "专业版（年付）",
    cycle: "yearly",
    priceCNY: 588,
    tagline: "年付约省 2 个月",
    features: ["专业版全部能力", "按年付费，比月付约省 2 个月"],
    caps: { skus: 5000, users: 5, warehouses: 3 },
  },
  {
    code: "enterprise",
    name: "企业版",
    cycle: "monthly",
    priceCNY: 198,
    tagline: "专属支持 · 优先响应",
    features: ["专业版全部能力", "1 对 1 客服 · 优先响应"],
    caps: { skus: -1, users: -1, warehouses: -1 },
  },
  {
    code: "enterprise_yearly",
    name: "企业版（年付）",
    cycle: "yearly",
    priceCNY: 1980,
    tagline: "企业版年付 · 再省 2 个月",
    features: ["企业版全部能力", "年付更优惠"],
    caps: { skus: -1, users: -1, warehouses: -1 },
  },
] as const

/** Format an integer CNY price as "¥58" / "免费". */
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
