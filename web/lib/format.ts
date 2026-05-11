/**
 * Centralised display formatters. Always reach for these instead of inline
 * `n.toFixed(2)` / hand-rolled `¥${n}` — they enforce one rounding rule, one
 * thousands separator policy, and one date locale across the product.
 *
 * Values may arrive as backend-precise strings (decimals come in as JSON
 * strings to avoid IEEE754 loss); all helpers tolerate string | number.
 */

const CNY = new Intl.NumberFormat("zh-CN", {
  style: "currency",
  currency: "CNY",
  maximumFractionDigits: 2,
  minimumFractionDigits: 2,
})

const DATE = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

const DATETIME = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
})

const PERCENT = new Intl.NumberFormat("zh-CN", {
  style: "percent",
  maximumFractionDigits: 2,
})

function toNumber(value: string | number | null | undefined): number | null {
  if (value === null || value === undefined || value === "") return null
  const n = typeof value === "number" ? value : Number(value)
  return Number.isFinite(n) ? n : null
}

/** Format a CNY amount: `1234.5` → `¥1,234.50`. Returns `—` for null/NaN. */
export function formatCNY(value: string | number | null | undefined): string {
  const n = toNumber(value)
  return n === null ? "—" : CNY.format(n)
}

/** Format a fixed-precision decimal: `1234.5` (3) → `1,234.500`. */
export function formatNumber(
  value: string | number | null | undefined,
  fractionDigits = 0,
): string {
  const n = toNumber(value)
  if (n === null) return "—"
  return new Intl.NumberFormat("zh-CN", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  }).format(n)
}

/** Format a percent value already expressed as a fraction (0.05 → 5%). */
export function formatPercent(value: string | number | null | undefined): string {
  const n = toNumber(value)
  return n === null ? "—" : PERCENT.format(n)
}

/** Format a date (date-only): ISO string / Date / epoch ms → `2026/05/10`. */
export function formatDate(value: string | number | Date | null | undefined): string {
  if (value === null || value === undefined || value === "") return "—"
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return "—"
  return DATE.format(d)
}

/** Format a date + time: `2026/05/10 14:30`. */
export function formatDateTime(value: string | number | Date | null | undefined): string {
  if (value === null || value === undefined || value === "") return "—"
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return "—"
  return DATETIME.format(d)
}
