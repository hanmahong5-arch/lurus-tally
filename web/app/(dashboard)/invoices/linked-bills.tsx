import type { InvoiceLink } from "@/lib/api/invoices"

const SHOWN = 2

/** 关联单据 cell: the bill numbers an invoice covers, the first few then "等N张". */
export function LinkedBills({ links }: { links?: InvoiceLink[] }) {
  const list = (links ?? []).filter((l) => l.bill_no)
  if (list.length === 0) return <span className="text-muted-foreground">—</span>
  const rest = list.length - SHOWN
  return (
    <span className="block font-mono text-xs" data-testid="linked-bills">
      {list.slice(0, SHOWN).map((l) => l.bill_no).join("、")}
      {rest > 0 && <span className="font-sans text-muted-foreground">{` 等 ${list.length} 张`}</span>}
    </span>
  )
}
