import type { BadgeTone } from "@/components/ui/badge"
import type { BillStatus } from "@/lib/api/purchase"

/**
 * Single source of truth: a bill status maps to exactly ONE badge tone, so the
 * same status reads the same colour on every page (purchases, sales, detail…).
 * This replaces the per-page STATUS_BADGE maps that drifted apart — approved
 * bills were green on purchases but blue on sales.
 */
export const BILL_STATUS_TONE: Record<BillStatus, BadgeTone> = {
  0: "neutral", // 草稿 draft
  2: "ok", // 已审核 approved
  9: "err", // 已取消 cancelled
}

export { BILL_STATUS_LABEL } from "@/lib/api/purchase"
