/** Three invoice amounts reconcile only when each is present and excl + tax = total. */
export function moneyReconciles(excl: string, tax: string, total: string): boolean {
  const e = cents(excl)
  const t = cents(tax)
  const g = cents(total)
  if (e === null || t === null || g === null) return false
  if (e <= 0 || g <= 0 || t < 0) return false
  return e + t === g
}

export function moneyCents(raw: string): number | null {
  return cents(raw)
}

function cents(raw: string): number | null {
  const t = raw.trim().replace(/,/g, "")
  if (!/^\d+(\.\d{1,4})?$/.test(t)) return null
  const [whole, frac = ""] = t.split(".")
  const padded = (frac + "0000").slice(0, 4)
  const value = Number(whole) * 10000 + Number(padded)
  if (!Number.isFinite(value)) return null
  return Math.round(value / 100)
}

/** How the confirmed total differs from one bill, in yuan. */
export function moneyGap(face: string, bill: string): string {
  const a = cents(face)
  const b = cents(bill)
  if (a === null || b === null) return ""
  return gapText(a - b, "这张单")
}

/** How the confirmed total differs from the sum of the checked bills. */
export function comparedTotal(face: string, parts: string[]): string {
  if (parts.length === 0) return ""
  const faceCents = cents(face)
  if (faceCents === null) return ""
  let sum = 0
  for (const part of parts) {
    const c = cents(part)
    if (c === null) return ""
    sum += c
  }
  return gapText(faceCents - sum, "勾选单据合计")
}

function gapText(delta: number, other: string): string {
  if (delta === 0) return `与${other}一致`
  const abs = (Math.abs(delta) / 100).toFixed(2)
  return delta > 0 ? `比${other}多 ${abs}` : `比${other}少 ${abs}`
}
