import type { PrintSettings } from "@/lib/api/print-settings"
import { money, price, qty } from "./print-format"
import type { DocumentLine } from "./document-sheet"
import { offlineRefOf, type QueueEntry } from "@/lib/pos/checkout-queue"

interface ServerReceiptProps {
  /** 缺省即服务器单据（兼容原有调用）。 */
  kind?: "server"
  shop: PrintSettings
  billNo: string
  dateText: string
  lines: DocumentLine[]
  totalAmount: string
  /** Amount booked as paid on the bill. */
  paidAmount?: string
  /** 现金、微信… already in words. */
  payMethod?: string
  /** Cash handed over (may exceed the total); defaults to the booked amount. */
  receivedAmount?: string
  changeAmount?: string
  unpaidAmount?: string
  /** Wider paper gets a slightly larger font. */
  wide: boolean
  stamp?: string
  /** 销售小票 by default; 退货小票 for a return. */
  title?: string
  remark?: string
}

/** 本机台账里的离线单：单号印 OFF-xxxx-n，并标明「离线单，待上传」。 */
interface OfflineReceiptProps {
  kind: "offline"
  entry: QueueEntry
  shop: PrintSettings
  wide: boolean
}

export type ReceiptSheetProps = ServerReceiptProps | OfflineReceiptProps

export const OFFLINE_STAMP = "离线单，待上传"

function pad2(n: number): string {
  return String(n).padStart(2, "0")
}

function offlineDateText(ms: number): string {
  const d = new Date(ms)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

const OFFLINE_PAY_LABEL: Record<string, string> = { cash: "现金", wechat: "微信", alipay: "支付宝", card: "银行卡", credit: "赊账", transfer: "转账" }

/** 台账条目 -> 小票字段：商品名取入队时记下的名字，实收/找零按请求体里的实收额推算。 */
function offlineToServerProps(entry: QueueEntry, shop: PrintSettings, wide: boolean): ServerReceiptProps {
  const b = entry.body
  const lines: DocumentLine[] = b.items.map((it) => {
    const qtyN = Number(it.qty)
    const priceN = Number(it.unit_price)
    return {
      name: entry.item_names?.[it.product_id] || "商品",
      unit: it.unit_name,
      qty: it.qty,
      price: it.unit_price,
      amount: (Number.isFinite(qtyN) && Number.isFinite(priceN) ? Math.round(qtyN * priceN * 100) / 100 : 0).toFixed(2),
    }
  })
  const total = entry.total_amount ?? "0"
  const paid = Number(b.paid_amount)
  const change = b.payment_method === "cash" && paid > Number(total) ? (paid - Number(total)).toFixed(2) : undefined
  return {
    shop,
    billNo: offlineRefOf(entry.client_ref),
    dateText: offlineDateText(entry.occurred_at),
    lines,
    totalAmount: total,
    paidAmount: b.paid_amount,
    payMethod: OFFLINE_PAY_LABEL[b.payment_method] ?? "其他方式",
    receivedAmount: b.payment_method === "cash" ? b.paid_amount : undefined,
    changeAmount: change,
    wide,
    stamp: OFFLINE_STAMP,
    remark: b.remark,
  }
}

/** 58 / 80 mm till receipt. Every row wraps, so nothing can run past the paper edge. */
export function ReceiptSheet(props: ReceiptSheetProps) {
  const p = props.kind === "offline" ? offlineToServerProps(props.entry, props.shop, props.wide) : props
  return <ReceiptBody {...p} />
}

function ReceiptBody({ shop, billNo, dateText, lines, totalAmount, paidAmount, payMethod, receivedAmount, changeAmount, unpaidAmount, wide, stamp, title = "销售小票", remark }: ServerReceiptProps) {
  const unpaid = Number(unpaidAmount ?? "0")
  const change = Number(changeAmount ?? "0")
  return (
    <div
      className="box-border w-full break-words px-[3mm] py-[3mm] text-black"
      style={{ fontSize: wide ? "13px" : "11px", lineHeight: 1.35 }}
    >
      <div className="text-center">
        <div className="text-[1.25em] font-bold">{shop.shop_name || title}</div>
        {shop.shop_name && <div>{title}</div>}
        {stamp && <div className="mx-auto mt-1 inline-block border border-black px-2 font-bold">{stamp}</div>}
        {shop.phone && <div>电话：{shop.phone}</div>}
        {shop.address && <div>{shop.address}</div>}
      </div>
      <div className="my-1.5 border-t border-dashed border-black" />
      <div>单号：{billNo}</div>
      <div>时间：{dateText}</div>
      <div className="my-1.5 border-t border-dashed border-black" />
      {lines.map((l, i) => (
        <div key={i} className="mb-1 break-inside-avoid">
          <div>{l.name}</div>
          <div className="flex justify-between gap-2 tabular-nums">
            <span>
              {qty(l.qty)}
              {l.unit ?? ""} × {price(l.price)}
            </span>
            <span>{money(l.amount)}</span>
          </div>
        </div>
      ))}
      <div className="my-1.5 border-t border-dashed border-black" />
      <div className="flex justify-between text-[1.15em] font-bold tabular-nums">
        <span>{title === "退货小票" ? "退款合计" : "合计"}</span>
        <span>¥{money(totalAmount)}</span>
      </div>
      {payMethod && (
        <div className="flex justify-between">
          <span>付款方式</span>
          <span>{payMethod}</span>
        </div>
      )}
      {paidAmount !== undefined && (
        <div className="flex justify-between tabular-nums">
          <span>实收</span>
          <span>¥{money(receivedAmount ?? paidAmount)}</span>
        </div>
      )}
      {change > 0 && (
        <div className="flex justify-between tabular-nums">
          <span>找零</span>
          <span>¥{money(changeAmount)}</span>
        </div>
      )}
      {unpaid > 0 && (
        <div className="flex justify-between tabular-nums">
          <span>赊欠</span>
          <span>¥{money(unpaidAmount)}</span>
        </div>
      )}
      {remark && <div className="mt-1">备注：{remark}</div>}
      <div className="my-1.5 border-t border-dashed border-black" />
      <div className="text-center">{shop.receipt_footer || "谢谢惠顾，欢迎再次光临"}</div>
    </div>
  )
}
