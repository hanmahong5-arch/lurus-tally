import { shanghaiToday } from "./date-range"

/**
 * Today's date on the shop's calendar (Asia/Shanghai) as YYYY-MM-DD. Use this
 * for every "default to today" form field: toISOString() is UTC and would show
 * yesterday before 08:00 local time.
 */
export function todayCST(now: Date = new Date()): string {
  return shanghaiToday(now)
}
