import type { Statement, StatementLine } from "@/lib/api/receipts"
import { PAY_TYPE_LABEL } from "@/lib/api/payment"
import { formatCNY, formatDate } from "@/lib/format"

/** Plain-Chinese name of a statement line; a payment that adds to the balance is a refund. */
export function lineLabel(direction: Statement["direction"], l: StatementLine): string {
  if (l.kind === "bill") return direction === "ar" ? "发货" : "进货"
  if (l.kind === "return") return direction === "ap" ? "退货冲抵" : "退货"
  if (Number(l.delta) > 0) return "退款"
  return direction === "ar" ? "收款" : "付款"
}

function signed(v: string): string {
  const n = Number(v)
  return n < 0 ? `-${formatCNY(Math.abs(n))}` : formatCNY(n)
}

/** The summary figures and movement table of one statement, shared by the page and the print view. */
export function StatementTable({ st, cellClass = "px-3 py-1.5" }: { st: Statement; cellClass?: string }) {
  const ar = st.direction === "ar"
  const sums: [string, string][] = [
    ["期初欠款", st.opening],
    [ar ? "本期发货" : "本期进货", st.billed],
    ["本期退货", st.returned],
    [ar ? "本期收款" : "本期付款", st.paid],
  ]
  if (Number(st.adjustment) !== 0) sums.push(["其他调整", st.adjustment])
  sums.push(["期末欠款", st.closing])
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
        {sums.map(([label, v]) => (
          <div key={label}>
            <div className="text-muted-foreground">{label}</div>
            <div className="font-mono font-semibold tabular-nums">{formatCNY(v)}</div>
          </div>
        ))}
      </div>
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-border text-left text-muted-foreground">
            <th className={`${cellClass} font-medium`}>日期</th>
            <th className={`${cellClass} font-medium`}>类型</th>
            <th className={`${cellClass} font-medium`}>单号</th>
            <th className={`${cellClass} font-medium`}>方式 / 备注</th>
            <th className={`${cellClass} text-right font-medium`}>金额</th>
            <th className={`${cellClass} text-right font-medium`}>欠款余额</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {st.lines.length === 0 && (
            <tr>
              <td colSpan={6} className={`${cellClass} text-center text-muted-foreground`}>
                这段时间没有往来
              </td>
            </tr>
          )}
          {st.lines.map((l, i) => (
            <tr key={`${l.bill_id ?? ""}-${l.kind}-${i}`}>
              <td className={cellClass}>{formatDate(l.date)}</td>
              <td className={cellClass}>{lineLabel(st.direction, l)}</td>
              <td className={cellClass}>{l.bill_no}</td>
              <td className={cellClass}>
                {[l.method ? (PAY_TYPE_LABEL[l.method] ?? l.method) : "", l.remark ?? ""].filter(Boolean).join(" ") || "—"}
              </td>
              <td className={`${cellClass} text-right font-mono tabular-nums`}>{signed(l.delta)}</td>
              <td className={`${cellClass} text-right font-mono tabular-nums`}>{formatCNY(l.balance)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
