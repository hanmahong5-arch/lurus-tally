import Decimal from "decimal.js"

/** Tax rates offered when registering an invoice. "" means the shop types excl / tax by hand. */
export const TAX_RATES: { value: string; label: string }[] = [
  { value: "", label: "手动填写" },
  { value: "0", label: "免税" },
  { value: "0.01", label: "1%" },
  { value: "0.03", label: "3%" },
  { value: "0.06", label: "6%" },
  { value: "0.09", label: "9%" },
  { value: "0.13", label: "13%" },
]

/**
 * Splits a tax-inclusive total into the amount excluding tax and the tax:
 * excl = total / (1 + rate) rounded to cents, tax = total - excl (so the two always add back
 * to the total). Null when the total or the rate is not usable.
 */
export function splitTaxInclusive(total: string, rate: string): { excl: string; tax: string } | null {
  if (!total.trim() || !rate.trim()) return null
  let t: Decimal
  let r: Decimal
  try {
    t = new Decimal(total.trim())
    r = new Decimal(rate)
  } catch {
    return null
  }
  if (!t.isFinite() || !r.isFinite() || t.isNegative() || r.isNegative()) return null
  const excl = t.div(r.plus(1)).toDecimalPlaces(2, Decimal.ROUND_HALF_UP)
  return { excl: excl.toFixed(2), tax: t.minus(excl).toFixed(2) }
}
