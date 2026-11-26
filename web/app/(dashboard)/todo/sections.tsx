import Link from "next/link"

import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import { formatCNY } from "@/lib/format"
import type { TodoBill } from "@/lib/api/todo"

export function SectionHeader({
  icon,
  title,
  count,
  href,
}: {
  icon: string
  title: string
  count: number
  href?: string
}) {
  return (
    <div className="mb-2 flex items-center justify-between">
      <h2 className="flex items-center gap-2 text-sm font-medium">
        <span aria-hidden="true">{icon}</span>
        {title}
        {count > 0 && <Badge tone="accent">{count}</Badge>}
      </h2>
      {href && (
        <Link href={href} className="text-xs text-muted-foreground hover:text-foreground">
          全部 →
        </Link>
      )}
    </div>
  )
}

export function BillSection({
  icon,
  title,
  count,
  items,
  allHref,
  detailHref,
  action,
  showAmount = true,
  note,
}: {
  icon: string
  title: string
  count: number
  items: TodoBill[]
  allHref: string
  detailHref: (id: string) => string
  action?: string
  showAmount?: boolean
  note?: string
}) {
  if (count === 0) return null
  return (
    <section>
      <SectionHeader icon={icon} title={title} count={count} href={allHref} />
      {note && <p className="mb-2 text-xs text-muted-foreground">{note}</p>}
      <ul className="divide-y divide-border rounded-xl border border-border bg-card">
        {items.map((b) => (
          <li key={b.id} className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{b.bill_no}</p>
              <p className="truncate text-xs text-muted-foreground">
                {b.partner_name || "散客"} · {b.bill_date}
              </p>
            </div>
            {showAmount && (
              <div className="text-right font-mono text-sm tabular-nums">{formatCNY(b.total_amount)}</div>
            )}
            {action && (
              <Link href={detailHref(b.id)} className={buttonVariants({ size: "sm" })}>
                {action}
              </Link>
            )}
          </li>
        ))}
      </ul>
      {count > items.length && (
        <p className="mt-1.5 text-center text-xs text-muted-foreground">
          还有 {count - items.length} 张，点右上角“全部”查看
        </p>
      )}
    </section>
  )
}
