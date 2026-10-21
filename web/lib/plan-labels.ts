import { TALLY_PLANS } from "@/lib/billing/plans"

/** Shop-facing names for the plan and subscription status codes the platform returns. */
const STATUS_LABELS: Record<string, string> = {
  active: "使用中",
  trial: "试用中",
  trialing: "试用中",
  pending: "待生效",
  grace: "宽限期内",
  grace_period: "宽限期内",
  suspended: "已暂停",
  expired: "已到期",
  cancelled: "已取消",
  canceled: "已取消",
}

export function planLabel(code: string | null | undefined): string {
  const key = (code || "free").toLowerCase()
  return TALLY_PLANS.find((p) => p.code === key)?.name ?? "其他套餐"
}

export function subscriptionStatusLabel(status: string | null | undefined): string {
  if (!status) return "未开通"
  return STATUS_LABELS[status.toLowerCase()] ?? "状态未知"
}
