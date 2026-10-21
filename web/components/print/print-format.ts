import Decimal from "decimal.js"

function dec(v: string | number | null | undefined): Decimal | null {
  if (v === null || v === undefined || v === "") return null
  try {
    const d = new Decimal(v)
    return d.isFinite() ? d : null
  } catch {
    return null
  }
}

/** Money to two places: `12.5` → `12.50`; unreadable → `—`. */
export function money(v: string | number | null | undefined): string {
  const d = dec(v)
  return d ? d.toDecimalPlaces(2, Decimal.ROUND_HALF_UP).toFixed(2) : "—"
}

/** Unit price keeps up to four places but never fewer than two: `3` → `3.00`, `3.125` → `3.125`. */
export function price(v: string | number | null | undefined): string {
  const d = dec(v)
  if (!d) return "—"
  const four = d.toDecimalPlaces(4, Decimal.ROUND_HALF_UP)
  return four.decimalPlaces() < 2 ? four.toFixed(2) : four.toFixed()
}

/** Quantity without trailing zeros: `2.0000` → `2`. */
export function qty(v: string | number | null | undefined): string {
  const d = dec(v)
  return d ? d.toDecimalPlaces(4, Decimal.ROUND_HALF_UP).toFixed() : "—"
}

/** Stamp for a bill that is not a live, approved document: 0 = draft, 9 = cancelled. */
export function statusStamp(status: number | undefined): string | undefined {
  if (status === 0) return "未审核"
  if (status === 9) return "已作废"
  return undefined
}

/** Joins the stamps that apply to one sheet (e.g. 退货 + 未审核). */
export function joinStamps(...stamps: (string | undefined)[]): string | undefined {
  const s = stamps.filter(Boolean).join(" ")
  return s || undefined
}

/** What is still owed, never negative: an over-collected sale shows 0, not -10. */
export function owedAmount(v: string | undefined): string {
  const d = dec(v)
  return d && d.gt(0) ? d.toFixed() : "0"
}

/** Remark for paper: system-written import remarks (`import:shopify:1001`) become a readable 网店订单 note. */
export function remarkText(v: string | undefined): string | undefined {
  const t = v?.trim()
  if (!t) return undefined
  const m = /^import:[a-z_]+:(.*)$/i.exec(t)
  if (!m) return t
  const no = m[1].trim()
  return no ? `网店订单 ${no}` : "网店订单"
}

const PAY_TYPE_LABEL: Record<string, string> = {
  cash: "现金",
  wechat: "微信",
  alipay: "支付宝",
  card: "银行卡",
  transfer: "转账",
  credit: "赊账",
}

/** Payment method for paper: distinct methods joined with 、; unknown ones read 其他方式. */
export function payTypeText(types: string[]): string {
  const labels = Array.from(new Set(types.filter(Boolean).map((t) => PAY_TYPE_LABEL[t] ?? "其他方式")))
  return labels.join("、")
}
