import type { PrintSettings } from "@/lib/api/print-settings"
import { moneyToChinese } from "@/lib/money-cn"
import { money, price, qty } from "./print-format"

export interface DocumentLine {
  name: string
  /** 规格, e.g. 胸径3cm 冠幅1.2m; the column only appears when some line has one. */
  spec?: string
  unit?: string
  qty: string
  price: string
  amount: string
}

interface DocumentSheetProps {
  shop: PrintSettings
  title: string
  /** Shown as a stamp next to the title, e.g. 退货. */
  stamp?: string
  billNo: string
  dateText: string
  /** Label / value pairs under the title (customer, supplier, warehouse). */
  meta: [string, string][]
  /** Full-width rows under `meta` (delivery address); a row with no value is left out. */
  metaWide?: [string, string][]
  lines: DocumentLine[]
  totalAmount: string
  /** Extra settlement lines under the total (已收款, 未收款). */
  settle?: [string, string][]
  remark?: string
  signLabels: string[]
  /** A5 gives the spec column more room and the price column less, so 胸径 / 冠幅 wrap in at most 2 lines. */
  paper?: "a4" | "a5"
}

/** Shared A4 / A5 layout for the delivery note and the purchase receipt note. */
export function DocumentSheet({
  shop,
  title,
  stamp,
  billNo,
  dateText,
  meta,
  metaWide,
  lines,
  totalAmount,
  settle,
  remark,
  signLabels,
  paper,
}: DocumentSheetProps) {
  const cell = "border border-black px-1.5 py-1"
  const big = moneyToChinese(totalAmount)
  const a5 = paper === "a5"
  const hasSpec = lines.some((l) => l.spec)
  const wideRows = (metaWide ?? []).filter(([, v]) => v)
  return (
    <div className="p-[8mm] text-[12px] leading-snug text-black">
      <div className="text-center">
        {shop.shop_name && <div className="text-[14px] font-medium">{shop.shop_name}</div>}
        <div className="flex items-center justify-center gap-3">
          <h1 className="text-[20px] font-bold tracking-widest">{title}</h1>
          {stamp && <span className="rounded border-2 border-black px-2 py-0.5 text-[14px] font-bold">{stamp}</span>}
        </div>
        {(shop.phone || shop.address) && (
          <div className="mt-0.5 text-[11px]">
            {shop.address}
            {shop.address && shop.phone ? "　" : ""}
            {shop.phone && `电话：${shop.phone}`}
          </div>
        )}
      </div>

      <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1">
        {meta.map(([k, v]) => (
          <div key={k}>
            {k}：{v || "—"}
          </div>
        ))}
        <div>单号：{billNo}</div>
        <div>日期：{dateText}</div>
        {wideRows.map(([k, v]) => (
          <div key={k} className="col-span-2 break-all">
            {k}：{v}
          </div>
        ))}
      </div>

      <table className="mt-2 w-full table-fixed border-collapse">
        <thead>
          <tr className="text-center">
            <th className={`${cell} w-8`}>序号</th>
            <th className={cell}>商品名称</th>
            {hasSpec && <th className={`${cell} ${a5 ? "w-24" : "w-16"} break-all`}>规格</th>}
            <th className={`${cell} w-12`}>单位</th>
            <th className={`${cell} w-20`}>数量</th>
            <th className={`${cell} ${a5 ? "w-16" : "w-20"}`}>单价</th>
            <th className={`${cell} w-28`}>金额</th>
          </tr>
        </thead>
        <tbody>
          {lines.map((l, i) => (
            <tr key={i} className="break-inside-avoid">
              <td className={`${cell} text-center`}>{i + 1}</td>
              <td className={`${cell} break-all`}>{l.name}</td>
              {hasSpec && <td className={`${cell} break-all text-center`}>{l.spec || ""}</td>}
              <td className={`${cell} text-center`}>{l.unit || ""}</td>
              <td className={`${cell} text-right tabular-nums`}>{qty(l.qty)}</td>
              <td className={`${cell} break-all text-right tabular-nums`}>{price(l.price)}</td>
              <td className={`${cell} text-right tabular-nums`}>{money(l.amount)}</td>
            </tr>
          ))}
          <tr className="break-inside-avoid font-medium">
            <td className={cell} colSpan={2}>
              合计金额（大写）
            </td>
            <td className={cell} colSpan={hasSpec ? 4 : 3}>
              {big}
            </td>
            <td className={`${cell} text-right tabular-nums`}>¥{money(totalAmount)}</td>
          </tr>
        </tbody>
      </table>

      {settle && settle.length > 0 && (
        <div className="mt-1 flex justify-end gap-6">
          {settle.map(([k, v]) => (
            <span key={k}>
              {k}：¥{money(v)}
            </span>
          ))}
        </div>
      )}
      {remark && <div className="mt-1">备注：{remark}</div>}

      <div className="mt-8 flex break-inside-avoid justify-between gap-4">
        {signLabels.map((s) => (
          <div key={s} className="flex-1 border-b border-black pb-1">
            {s}：
          </div>
        ))}
      </div>
    </div>
  )
}
