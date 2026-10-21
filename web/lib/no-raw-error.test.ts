// @vitest-environment node
import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative, sep } from "node:path"
import { describe, expect, it } from "vitest"

// Showing String(e) / e.message on the page leaks "ApiError: ..." or English transport text to
// shop owners. Failures must go through extractApiError(e).message instead.
const ROOT = join(__dirname, "..")
const SCAN_DIRS = ["app", "components", "lib"]

// Pages outside the main path that still use the raw form; shrink this list, never grow it.
const LEGACY = new Set([
  "app/(dashboard)/finance/exchange-rates/page.tsx",
  "app/(dashboard)/settings/shopify/page.tsx",
])

// setError(String(e)), setXxxError(... String(err)), toast.error("..." + String(e)),
// and the `err instanceof Error ? err.message : String(err)` fallback.
const RAW_PATTERNS = [
  /\b(?:set\w*Error|toast\.error)\([^;\n]*\bString\(\s*(?:e|err|error|ex)\s*\)/,
  /instanceof Error \? \w+\.message : String\(\s*\w+\s*\)/,
]

export function findRawErrorUse(source: string): string[] {
  return source
    .split("\n")
    .filter((line) => RAW_PATTERNS.some((re) => re.test(line)))
    .map((line) => line.trim())
}

function walk(dir: string, out: string[]) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(p)
  }
}

describe("no raw error text on the page", () => {
  it("detects the forbidden patterns", () => {
    expect(findRawErrorUse("setError(String(e))")).toHaveLength(1)
    expect(findRawErrorUse('toast.error("删除失败：" + String(err))')).toHaveLength(1)
    expect(findRawErrorUse("setCreateError(err instanceof Error ? err.message : String(err))")).toHaveLength(1)
    expect(findRawErrorUse("setError(extractApiError(e).message)")).toHaveLength(0)
    expect(findRawErrorUse("setCount(String(n))")).toHaveLength(0)
  })

  it("keeps String(e) out of the source", () => {
    const files: string[] = []
    for (const d of SCAN_DIRS) walk(join(ROOT, d), files)
    const offenders: string[] = []
    for (const f of files) {
      const rel = relative(ROOT, f).split(sep).join("/")
      if (LEGACY.has(rel)) continue
      const hits = findRawErrorUse(readFileSync(f, "utf8"))
      if (hits.length > 0) offenders.push(`${rel}: ${hits[0]}`)
    }
    expect(offenders).toEqual([])
  })

  it("does not exempt files that are already clean", () => {
    const stale = Array.from(LEGACY).filter((rel) => findRawErrorUse(readFileSync(join(ROOT, rel), "utf8")).length === 0)
    expect(stale).toEqual([])
  })
})
