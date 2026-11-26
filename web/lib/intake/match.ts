import { moneyCents } from "./money"

export interface MatchBill {
  id: string
  status: number
  remaining?: string
}

const FEN = 1

function near(a: number, b: number): boolean {
  return Math.abs(a - b) <= FEN
}

/** Same order as the server: one exact bill, then two, then three, else oldest that still fit. */
export function suggestBillIds(total: string, bills: MatchBill[]): string[] {
  const face = moneyCents(total)
  const usable = bills.filter((b) => b.status === 2 && (moneyCents(b.remaining || "") ?? 0) > 0)
  if (face === null || face <= 0 || usable.length === 0) return []
  const rows = usable.map((b) => ({ id: b.id, cents: moneyCents(b.remaining || "") ?? 0 }))
  for (const n of [1, 2, 3]) {
    const hit = exact(rows, face, n)
    if (hit) return hit
  }
  const ids: string[] = []
  let sum = 0
  for (const row of rows) {
    if (sum + row.cents > face + FEN) continue
    ids.push(row.id)
    sum += row.cents
    if (near(sum, face)) break
  }
  return ids
}

function exact(rows: { id: string; cents: number }[], face: number, n: number): string[] | null {
  const ids: string[] = []
  const walk = (start: number, left: number, sum: number): string[] | null => {
    if (left === 0) return near(sum, face) ? [...ids] : null
    for (let i = start; i <= rows.length - left; i++) {
      ids.push(rows[i].id)
      const got = walk(i + 1, left - 1, sum + rows[i].cents)
      if (got) return got
      ids.pop()
    }
    return null
  }
  return walk(0, n, 0)
}

/** How the invoice total sits against the checked bills' uninvoiced amounts. */
export function coverNote(total: string, remainings: string[]): string {
  const face = moneyCents(total)
  if (face === null || remainings.length === 0) return ""
  let sum = 0
  for (const raw of remainings) {
    const c = moneyCents(raw)
    if (c === null) return ""
    sum += c
  }
  const delta = face - sum
  if (delta === 0) return "勾选的未开票金额和这张发票一致"
  if (delta > 0) return `还有 ${(delta / 100).toFixed(2)} 元没有单据可对`
  return `多出的 ${(Math.abs(delta) / 100).toFixed(2)} 元留在单据上，不会整笔记到这张发票`
}
