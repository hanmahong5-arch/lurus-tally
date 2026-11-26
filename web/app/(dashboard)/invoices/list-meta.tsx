import type { BadgeTone } from "@/components/ui/badge"
import type { TabItem } from "@/components/ui/tabs"
import type { InvoiceDirection, InvoiceState } from "@/lib/api/invoices"

export const PAGE_SIZE = 20

export const DIRECTION_TABS: TabItem<InvoiceDirection | "">[] = [
  { label: "全部", value: "" },
  { label: "销项（开给客户）", value: "out" },
  { label: "进项（供应商开来）", value: "in" },
]

export const STATE_TONE: Record<InvoiceState, BadgeTone> = { issued: "ok", void: "neutral", red: "err" }

export const SELECT_CLASS = "h-8 rounded-lg border border-input bg-background px-2 text-sm"
