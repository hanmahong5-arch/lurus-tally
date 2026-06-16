/**
 * UAT — ⌘K Command Palette E2E
 *
 * Boundary: this file and uat-palette.config.ts only.
 *
 * Auth note: the Next.js FE proxy (/api/proxy/*) requires a NextAuth session
 * and will return 401 without one. The backend at http://localhost:18200 runs
 * with auth disabled in UAT mode (ZITADEL_DOMAIN unset). Therefore:
 *   - API-level checks (seeding, search latency) use the `playwright` fixture
 *     to open a new APIRequestContext pointed directly at the backend.
 *   - UI tests load the dashboard page directly; because the FE also has no
 *     auth gate in dev mode, the palette shortcut is triggered without login.
 *     If the page redirects to /login the auth-blocker test will surface this.
 *
 * Telemetry note: trackEvent sends to /api/otel-events (NOT /api/proxy/*).
 * The route returns { ok: true } even with no collector configured — safe
 * for local dev.
 */

import * as nodePath from "node:path"
import { test, expect, type Page, type APIRequestContext } from "@playwright/test"
import { gateOnDevServer } from "./_server-health"

// ─── Constants ─────────────────────────────────────────────────────────────

const BACKEND = "http://localhost:18200"

// REAL mode (UAT_REAL=1, set by uat-stage.config.ts): API calls go through the
// session-authenticated FE proxy (/api/proxy/*) to the STAGE backend instead
// of a local auth-disabled backend. When UAT_REAL is unset the original stub
// path below is 100% unchanged.
const REAL = process.env.UAT_REAL === "1"

/** Maps a direct-backend /api/v1 path to its FE-proxy equivalent in REAL mode. */
function apiPath(p: string): string {
  return REAL ? p.replace(/^\/api\/v1\//, "/api/proxy/") : p
}
const SCREENSHOT_DIR = nodePath.join(
  __dirname,
  "..",
  "..",
  "test-results-uat",
  "screenshots"
)

const PALETTE_TESTID = "command-palette"
const INPUT_TESTID   = "palette-input"

const SEED_PRODUCT = {
  code: "UAT-TEST-001",
  name: "测试商品 UAT",
  measurement_strategy: "individual",
}

// ─── Helpers ───────────────────────────────────────────────────────────────

async function gotoDashboard(page: Page): Promise<void> {
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" })
  await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => {})
}

async function openPalette(page: Page): Promise<void> {
  await page.keyboard.press("Control+K")
  await page.waitForSelector(`[data-testid="${PALETTE_TESTID}"]`, {
    state: "visible",
    timeout: 2_000,
  })
}

async function closePalette(page: Page): Promise<void> {
  await page.keyboard.press("Escape")
  await page
    .waitForSelector(`[data-testid="${PALETTE_TESTID}"]`, {
      state: "detached",
      timeout: 2_000,
    })
    .catch(() => {})
}

/**
 * Seed one product via direct backend HTTP.
 * Uses the Playwright test-level APIRequestContext which targets BACKEND.
 * Idempotent — 409 Conflict is treated as success.
 */
async function seedProduct(ctx: APIRequestContext): Promise<string | null> {
  const res = await ctx.post(apiPath("/api/v1/products"), {
    data: SEED_PRODUCT,
    headers: { "Content-Type": "application/json" },
  })
  if (res.ok()) {
    const body = (await res.json()) as { id?: string }
    return body.id ?? null
  }
  if (res.status() === 409) {
    const listRes = await ctx.get(
      apiPath(`/api/v1/products?q=${encodeURIComponent(SEED_PRODUCT.code)}&limit=1`)
    )
    if (listRes.ok()) {
      const body = (await listRes.json()) as { data?: Array<{ id: string }> }
      return body.data?.[0]?.id ?? null
    }
  }
  console.warn(
    `[seed] product creation returned ${res.status()}: ${await res.text()}`
  )
  return null
}

// ─── Fixtures: backend APIRequestContext ────────────────────────────────────
// We extend the base test with a `backendApi` fixture that targets BACKEND
// directly — bypassing the Next.js proxy and its auth requirement.

const paletteTest = test.extend<{ backendApi: APIRequestContext }>({
  backendApi: async ({ playwright, context }, use) => {
    if (REAL) {
      // Browser-context request — carries the NextAuth session cookie from
      // storageState and resolves relative URLs against baseURL, so calls go
      // through /api/proxy/* to the real STAGE backend.
      await use(context.request)
      return
    }
    const ctx = await playwright.request.newContext({ baseURL: BACKEND })
    await use(ctx)
    await ctx.dispose()
  },
})

// ─── Tests ─────────────────────────────────────────────────────────────────

// Skip (not fail) when the dev server has crashed/restarted — env, not product.
gateOnDevServer(paletteTest)

paletteTest.describe("Command Palette UAT", () => {

  // ── 1. Open via Ctrl+K within 100 ms ─────────────────────────────────────
  paletteTest(
    "1 — palette opens via Ctrl+K within 100 ms",
    async ({ page }) => {
      await gotoDashboard(page)

      const t0 = Date.now()
      await page.keyboard.press("Control+K")
      const palette = page.getByTestId(PALETTE_TESTID)
      await expect(palette).toBeVisible({ timeout: 2_000 })
      const elapsed = Date.now() - t0

      await page.screenshot({
        path: nodePath.join(SCREENSHOT_DIR, "uat-palette-open.png"),
        fullPage: false,
      })

      expect(
        elapsed,
        `Palette should appear within 100 ms; actual: ${elapsed} ms`
      ).toBeLessThan(100)

      await closePalette(page)
    }
  )

  // ── 2. Three column groups visible ───────────────────────────────────────
  paletteTest(
    "2 — three column groups appear (Commands, Entity, AI)",
    async ({ page }) => {
      await gotoDashboard(page)
      await openPalette(page)

      const palette = page.getByTestId(PALETTE_TESTID)

      // Static groups "pages" and "actions" are always present.
      await expect(palette.getByText("页面")).toBeVisible()
      await expect(palette.getByText("操作")).toBeVisible()

      // Type ≥5 chars to trigger the AI sentinel.
      const input = palette.getByTestId(INPUT_TESTID)
      await input.fill("testproduct12345")

      await expect(palette.getByText("AI 模式", { exact: true })).toBeVisible({ timeout: 3_000 })

      // Entity group appears when the backend has results; warn if absent.
      const entityVisible = await palette
        .getByText("实体")
        .isVisible({ timeout: 2_000 })
        .catch(() => false)
      if (!entityVisible) {
        console.warn(
          "[UAT-2] Entity group header not visible — DB may be empty. " +
            "Will surface in test 3 if seed also fails."
        )
      }

      await page.screenshot({
        path: nodePath.join(SCREENSHOT_DIR, "uat-palette-three-groups.png"),
        fullPage: false,
      })

      // Hard assertions: Commands (pages + actions) + AI must always be present.
      await expect(palette.getByText("页面")).toBeVisible()
      await expect(palette.getByText("操作")).toBeVisible()
      await expect(palette.getByText("AI 模式", { exact: true })).toBeVisible()

      await closePalette(page)
    }
  )

  // ── 3. Typing returns entity results ─────────────────────────────────────
  paletteTest(
    "3 — typing returns entity results (seeds product if DB empty)",
    async ({ page, backendApi }) => {
      const productId = await seedProduct(backendApi)
      if (productId == null) {
        console.warn(
          "[UAT-3] Could not seed product — backend may require auth. " +
            "Entity results may be absent."
        )
      }

      await gotoDashboard(page)
      await openPalette(page)

      const palette = page.getByTestId(PALETTE_TESTID)
      const input = palette.getByTestId(INPUT_TESTID)

      await input.type("t", { delay: 20 })

      // Allow 150 ms debounce + network round-trip; wait up to 2 s.
      const entityOption = palette.locator('[role="option"]').first()
      const appeared = await entityOption
        .isVisible({ timeout: 2_000 })
        .catch(() => false)

      await page.screenshot({
        path: nodePath.join(SCREENSHOT_DIR, "uat-palette-entity-results.png"),
        fullPage: false,
      })

      if (!appeared) {
        throw new Error(
          "[UAT-3 FAIL] No entity items appeared after typing. " +
            "Either backend search returns no rows (check seed) or the FE entity column is broken. " +
            `Backend seed result: productId=${productId ?? "null"}. ` +
            "Screenshot: test-results-uat/screenshots/uat-palette-entity-results.png"
        )
      }

      await expect(palette.locator('[role="option"]').first()).toBeVisible()
      await closePalette(page)
    }
  )

  // ── 4. p95 keystroke→search response < 200 ms (20 samples) ───────────────
  paletteTest(
    "4 — p95 keystroke→search response < 200 ms (20 samples)",
    async ({ backendApi }) => {
      const queries = "abcdefghijklmnopqrstu".split("").slice(0, 20)
      const samples: number[] = []

      for (const char of queries) {
        const t0 = performance.now()
        const res = await backendApi.get(
          apiPath(`/api/v1/search?q=${encodeURIComponent(char)}&limit=5`)
        )
        const elapsed = performance.now() - t0
        samples.push(elapsed)

        if (!res.ok() && res.status() !== 401) {
          console.warn(`[UAT-4] search returned ${res.status()} for q=${char}`)
        }
      }

      samples.sort((a, b) => a - b)
      const p50 = samples[Math.floor(samples.length * 0.5)]
      const p95 = samples[Math.floor(samples.length * 0.95)]
      const p99 = samples[Math.floor(samples.length * 0.99)]

      console.log(
        `[UAT-4] Latency (${samples.length} samples) — ` +
          `p50: ${p50.toFixed(1)} ms | p95: ${p95.toFixed(1)} ms | p99: ${p99.toFixed(1)} ms`
      )

      if (REAL) {
        // Through next-dev proxy + WAN to STAGE the 200 ms budget does not
        // apply — percentiles are informational. The strict budget remains
        // asserted in the local (stub) config run.
        console.log(
          "[UAT-4] REAL mode: p95 budget not asserted (proxy + WAN path); see logged percentiles"
        )
        return
      }

      expect(
        p95,
        `p95 must be < 200 ms. ` +
          `Actual p50=${p50.toFixed(1)} ms, p95=${p95.toFixed(1)} ms, p99=${p99.toFixed(1)} ms`
      ).toBeLessThan(200)
    }
  )

  // ── 5. Esc closes palette ─────────────────────────────────────────────────
  paletteTest("5 — Esc closes the palette", async ({ page }) => {
    await gotoDashboard(page)
    await openPalette(page)

    await expect(page.getByTestId(PALETTE_TESTID)).toBeVisible()

    await page.keyboard.press("Escape")

    // CommandPalette returns null when !open → element is removed from DOM.
    await expect(page.getByTestId(PALETTE_TESTID)).toBeHidden({ timeout: 2_000 })
  })

  // ── 6. Selecting entity navigates to detail page ──────────────────────────
  paletteTest(
    "6 — selecting a product entity navigates to /products/:id",
    async ({ page, backendApi }) => {
      const productId = await seedProduct(backendApi)

      await gotoDashboard(page)
      await openPalette(page)

      const palette = page.getByTestId(PALETTE_TESTID)
      const input = palette.getByTestId(INPUT_TESTID)

      await input.fill("UAT")

      const productItem = palette
        .locator(`[data-testid^="palette-item-entity-product-"]`)
        .first()
      const appeared = await productItem
        .isVisible({ timeout: 3_000 })
        .catch(() => false)

      if (!appeared) {
        const anyOption = palette.locator('[role="option"]').first()
        const anyVisible = await anyOption
          .isVisible({ timeout: 1_000 })
          .catch(() => false)
        if (!anyVisible) {
          console.warn(
            "[UAT-6] No entity items visible after searching 'UAT'. " +
              "Navigation test skipped — not false-green."
          )
          paletteTest.skip()
          return
        }
        await anyOption.click()
      } else {
        await productItem.click()
      }

      await page.waitForURL(
        (url) =>
          url.pathname.startsWith("/products/") ||
          url.pathname.startsWith("/suppliers/") ||
          url.pathname.startsWith("/purchases/"),
        { timeout: 5_000 }
      )

      const finalUrl = page.url()
      console.log(`[UAT-6] Navigated to: ${finalUrl}`)

      if (productId) {
        expect(finalUrl).toContain(productId)
      }

      await page.screenshot({
        path: nodePath.join(SCREENSHOT_DIR, "uat-palette-navigate.png"),
        fullPage: false,
      })
    }
  )

  // ── 7. Telemetry fires palette_invocation to /api/otel-events ─────────────
  paletteTest(
    "7 — palette_invocation telemetry fires to /api/otel-events",
    async ({ page }) => {
      await gotoDashboard(page)

      const telemetryRequests: Array<{ url: string; body: unknown }> = []
      page.on("request", (req) => {
        if (req.url().includes("otel-events") && req.method() === "POST") {
          let body: unknown = null
          try {
            body = JSON.parse(req.postData() ?? "null")
          } catch {
            /* ignore parse errors */
          }
          telemetryRequests.push({ url: req.url(), body })
        }
      })

      await openPalette(page)

      const palette = page.getByTestId(PALETTE_TESTID)
      const input = palette.getByTestId(INPUT_TESTID)

      // Type enough to trigger entity search; palette_invocation fires when
      // entity results appear for the first time.
      await input.fill("test")
      // Allow debounce (150 ms) + render + any async tracking to complete.
      await page.waitForTimeout(600)

      // Closing always fires palette_invocation with action_picked.
      await closePalette(page)
      await page.waitForTimeout(200)

      const invocationEvents = telemetryRequests.filter((r) => {
        if (typeof r.body !== "object" || r.body === null) return false
        const b = r.body as Record<string, unknown>
        return b["event"] === "palette_invocation"
      })

      console.log(
        `[UAT-7] Total otel-events requests: ${telemetryRequests.length}; ` +
          `palette_invocation count: ${invocationEvents.length}`
      )

      if (invocationEvents.length === 0) {
        throw new Error(
          `[UAT-7 FAIL] No palette_invocation event sent to /api/otel-events. ` +
            `Observed events: ${JSON.stringify(
              telemetryRequests.map(
                (r) => (r.body as Record<string, unknown>)?.["event"]
              )
            )}`
        )
      }

      expect(invocationEvents.length).toBeGreaterThanOrEqual(1)

      const evt = invocationEvents[0].body as Record<string, unknown>
      const meta = evt["metadata"] as Record<string, unknown> | undefined
      expect(meta).toBeDefined()
      expect(typeof meta?.["latency_ms"]).toBe("number")
      expect(["navigate", "query", "execute", "none"]).toContain(
        meta?.["action_picked"]
      )
    }
  )
})
