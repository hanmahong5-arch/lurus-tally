import Decimal from "decimal.js"

const DIGITS = ["零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"]
const POS_UNITS = ["", "拾", "佰", "仟"]
const SECTION_UNITS = ["", "万", "亿", "万亿"]
const MAX_INT_DIGITS = 16

/**
 * moneyToChinese turns an amount into capital Chinese numerals as printed on
 * delivery notes: `1234.50` → `壹仟贰佰叁拾肆元伍角`, `105` → `壹佰零伍元整`.
 * Rounds to fen (half-up); negatives get a leading `负`. Returns "" for input
 * that is not a number or is too large to read out, so callers can hide the line.
 */
export function moneyToChinese(value: string | number | null | undefined): string {
  if (value === null || value === undefined || value === "") return ""
  let amount: Decimal
  try {
    amount = new Decimal(value).toDecimalPlaces(2, Decimal.ROUND_HALF_UP)
  } catch {
    return ""
  }
  if (!amount.isFinite()) return ""

  const negative = amount.isNegative() && !amount.isZero()
  const [intPart, fracPart = "00"] = amount.abs().toFixed(2).split(".")
  if (intPart.length > MAX_INT_DIGITS) return ""

  const intNonZero = intPart !== "0"
  let out = ""
  if (intNonZero) {
    let zeroPending = false
    let sectionHasValue = false
    for (let i = 0; i < intPart.length; i++) {
      const pos = intPart.length - 1 - i
      const d = Number(intPart[i])
      if (d === 0) {
        zeroPending = true
      } else {
        if (zeroPending && out !== "") out += "零"
        zeroPending = false
        out += DIGITS[d] + POS_UNITS[pos % 4]
        sectionHasValue = true
      }
      if (pos % 4 === 0) {
        if (sectionHasValue) out += SECTION_UNITS[pos / 4]
        sectionHasValue = false
      }
    }
    out += "元"
  }

  const jiao = Number(fracPart[0])
  const fen = Number(fracPart[1])
  if (jiao === 0 && fen === 0) {
    out += intNonZero ? "整" : "零元整"
  } else {
    if (jiao > 0) out += DIGITS[jiao] + "角"
    if (fen > 0) out += (jiao === 0 && intNonZero ? "零" : "") + DIGITS[fen] + "分"
  }
  return (negative ? "负" : "") + out
}
