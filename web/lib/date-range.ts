/**
 * Date-range presets for list filters. All presets are computed on the
 * Asia/Shanghai calendar and returned as YYYY-MM-DD strings (inclusive), so a
 * shop owner sees "本月" the way the shop's calendar does regardless of the
 * browser's time zone.
 */

export type RangePreset = "today" | "this_week" | "this_month" | "last_month" | "all" | "custom"

export interface DateRange {
  from: string
  to: string
}

export const RANGE_PRESET_LABEL: Record<RangePreset, string> = {
  today: "今天",
  this_week: "本周",
  this_month: "本月",
  last_month: "上月",
  all: "全部",
  custom: "自定义",
}

export const RANGE_PRESETS: RangePreset[] = ["today", "this_week", "this_month", "last_month", "all", "custom"]

const TZ = "Asia/Shanghai"
const DAY_FMT = new Intl.DateTimeFormat("en-CA", {
  timeZone: TZ,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

const pad = (n: number) => String(n).padStart(2, "0")

function fmt(y: number, m: number, d: number): string {
  return `${y}-${pad(m)}-${pad(d)}`
}

/** Today's date on the Asia/Shanghai calendar, as YYYY-MM-DD. */
export function shanghaiToday(now: Date = new Date()): string {
  return DAY_FMT.format(now)
}

function parts(day: string): [number, number, number] {
  const [y, m, d] = day.split("-").map(Number)
  return [y, m, d]
}

/** Pure calendar arithmetic on UTC so the host time zone never leaks in. */
function addDays(day: string, delta: number): string {
  const [y, m, d] = parts(day)
  const t = new Date(Date.UTC(y, m - 1, d + delta))
  return fmt(t.getUTCFullYear(), t.getUTCMonth() + 1, t.getUTCDate())
}

function lastDayOfMonth(y: number, m: number): number {
  return new Date(Date.UTC(y, m, 0)).getUTCDate()
}

export function resolveDateRange(
  preset: RangePreset,
  now: Date = new Date(),
  custom?: Partial<DateRange>,
): DateRange {
  if (preset === "all") return { from: "", to: "" }
  if (preset === "custom") return { from: custom?.from ?? "", to: custom?.to ?? "" }
  const today = shanghaiToday(now)
  const [y, m, d] = parts(today)
  switch (preset) {
    case "today":
      return { from: today, to: today }
    case "this_week": {
      const dow = new Date(Date.UTC(y, m - 1, d)).getUTCDay() // 0 = Sunday
      const monday = addDays(today, -((dow + 6) % 7))
      return { from: monday, to: addDays(monday, 6) }
    }
    case "this_month":
      return { from: fmt(y, m, 1), to: fmt(y, m, lastDayOfMonth(y, m)) }
    case "last_month": {
      const py = m === 1 ? y - 1 : y
      const pm = m === 1 ? 12 : m - 1
      return { from: fmt(py, pm, 1), to: fmt(py, pm, lastDayOfMonth(py, pm)) }
    }
  }
}
