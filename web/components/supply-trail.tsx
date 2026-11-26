import Link from "next/link"

import { qtyText } from "@/lib/sale-from-purchase"

export interface SupplySale {
  id: string
  bill_no: string
  status: number
  partner_name?: string
}

const STATUS: Record<number, string> = { 0: "草稿", 2: "已出库", 9: "已作废" }

export function OpenedSales({ sales }: { sales?: SupplySale[] }) {
  if (!sales?.length) return null
  return (
    <section>
      <h2 className="mb-2 text-sm font-medium">由此开出的销售单</h2>
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {sales.map((s) => (
          <li key={s.id} className="flex items-center justify-between gap-3 px-4 py-3 text-sm">
            <Link href={`/sales/${s.id}`} className="font-mono text-primary hover:underline">
              {s.bill_no}
            </Link>
            <span className="text-muted-foreground">{s.partner_name || "未选客户"}</span>
            <span>{STATUS[s.status] ?? ""}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}

export function ReceiptLeft({
  items,
  names,
}: {
  items: { product_id: string; unit_name?: string; batch_remaining?: string }[]
  names: Record<string, string>
}) {
  const tracked = items.filter((it) => it.batch_remaining !== undefined)
  if (tracked.length === 0) return null
  return (
    <p className="text-xs text-muted-foreground">
      {tracked
        .map((it) => `${names[it.product_id] || "商品"}这一批还剩 ${qtyText(it.batch_remaining ?? "0")}${it.unit_name ? " " + it.unit_name : ""}`)
        .join("，")}
    </p>
  )
}

export function SourcePurchase({
  source,
}: {
  source?: { id: string; bill_no: string; partner_name?: string } | null
}) {
  if (!source) return null
  return (
    <p className="text-sm text-muted-foreground">
      货来自
      {source.partner_name ? ` ${source.partner_name} 的` : ""}
      进货单{" "}
      <Link href={`/purchases/${source.id}`} className="font-mono text-primary hover:underline">
        {source.bill_no}
      </Link>
    </p>
  )
}
