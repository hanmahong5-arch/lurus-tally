/**
 * Amounts are kept to the cent, matching the server: half rounds away from zero.
 * The toPrecision step strips binary noise first (1.005 * 100 = 100.49999999999999).
 */
export function roundCents(value: number): number {
  if (!Number.isFinite(value)) return 0
  const scaled = Number((Math.abs(value) * 100).toPrecision(12))
  return (Math.sign(value) * Math.round(scaled)) / 100
}

/** One line's amount: quantity times unit price, to the cent. */
export function lineCents(qty: number, unitPrice: number): number {
  return roundCents(qty * unitPrice)
}
