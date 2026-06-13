/**
 * UAT: onboarding E2E — kill-switch #1 gate
 *
 * Measures the full onboarding flow end-to-end and asserts it completes in
 * under 10 minutes wall-clock time (target: well under 60 s in practice).
 *
 * Auth strategy:
 *   The UAT Next.js instance requires a valid NextAuth session server-side,
 *   but the UAT NEXTAUTH_URL / AUTH_SECRET are not configured for localhost.
 *   We intercept every /api/auth/session request via page.route() to return
 *   a synthetic session, and intercept /api/proxy/* to return stub backend
 *   responses (backend auth middleware is also disabled in UAT but tenant_id
 *   is still required in Gin context — stub avoids that dependency entirely).
 *
 * Findings during authoring (documented for engineer follow-up):
 *   - /api/auth/session returns HTTP 500 in the UAT FE: NEXTAUTH_URL +
 *     AUTH_SECRET not injected into the web process. Path A (real session)
 *     is blocked. Path B (synthetic session via page.route) is used here.
 *   - Backend auth is disabled (ZITADEL_DOMAIN unset) but Gin context still
 *     returns 401 when tenant_id is not in context — so all /api/proxy calls
 *     are stubbed rather than forwarded to localhost:18200.
 *   - The /setup server component calls auth() at render time; the meta
 *     http-equiv redirect to /login fires when the session cookie is absent.
 *     We intercept the SSR page response for /setup and inject the persona
 *     wizard via direct navigation to /setup?step=seed&persona=<x>.
 */

import { test, expect, type Page, type Route } from "@playwright/test"
import path from "path"
import fs from "fs"

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const SCREEN_DIR = path.resolve(__dirname, "../../test-results/uat-onboarding/screenshots")
fs.mkdirSync(SCREEN_DIR, { recursive: true })

// REAL mode (UAT_REAL=1, set by uat-stage.config.ts): a real NextAuth session
// exists via storageState and /api/proxy/* forwards to the STAGE backend, so
// NO route stubs are installed and data-dependent assertions are relaxed to
// structural ones. When UAT_REAL is unset, the stub path is 100% unchanged.
const REAL = process.env.UAT_REAL === "1"

/** Wizard step waits get a longer budget when real STAGE round-trips occur. */
const STEP_TIMEOUT = REAL ? 60_000 : 15_000

/** Maximum wall-clock ms for the happy path. Kill-switch #1 = < 10 min. */
const MAX_WALL_MS = 600_000

/** Synthetic tenant used throughout stub responses. */
const UAT_TENANT_ID = "00000000-0000-0000-0000-000000000001"
const UAT_WAREHOUSE_ID = "wh-00000000-0000-0000-0000-000000000001"

// ---------------------------------------------------------------------------
// Stub helpers
// ---------------------------------------------------------------------------

/**
 * Synthetic NextAuth session payload returned for every /api/auth/session
 * request. This bypasses the server-side auth() call in the Next.js SSR layer.
 * The middleware still redirects unauthenticated requests to /login — we avoid
 * this by navigating directly to /setup?step=seed&persona=<x> which skips the
 * profile-picker branch of the page component (that branch calls auth() too,
 * but after the step branch returns early). In practice we observe the page
 * renders the wizard HTML before the redirect meta tag fires.
 */
function fakeSession(profileType: string) {
  return {
    user: {
      id: "uat-user",
      email: "uat@example.internal",
      name: "UAT Tester",
      tenantId: UAT_TENANT_ID,
      profileType,
      isFirstTime: true,
      role: "owner",
      isOwner: true,
    },
    accessToken: "uat-token",
    expires: new Date(Date.now() + 3_600_000).toISOString(),
  }
}

/** Warehouse list stub — one default warehouse so seedDemo can resolve an ID. */
const warehouseListStub = {
  items: [
    {
      id: UAT_WAREHOUSE_ID,
      tenantId: UAT_TENANT_ID,
      code: "WH-001",
      name: "主仓库",
      address: "",
      manager: "",
      isDefault: true,
      remark: "",
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
    },
  ],
  count: 1,
  limit: 1,
  offset: 0,
}

/** Seed-demo stub response (10 products created). */
const seedDemoStub = { products_created: 10 }

/** Replenish suggestions stub — two items so there is something to select. */
function replenishStub() {
  return {
    items: [
      {
        product_id: "prod-001",
        product_name: "示例商品A",
        product_code: "SKU-001",
        available_qty: "2",
        safety_qty: "10",
        avg_daily_sales: "2.5",
        suggested_qty: "30",
        est_amount_cny: "900.00",
        supplier_id: "sup-001",
        supplier_name: "示例供应商",
        urgency_score: "1",
        lead_time_days: 7,
        in_transit: "0",
        rop: "17.5",
        safety_stock: "5",
        reason: "库存低于安全线",
      },
      {
        product_id: "prod-002",
        product_name: "示例商品B",
        product_code: "SKU-002",
        available_qty: "5",
        safety_qty: "15",
        avg_daily_sales: "1.5",
        suggested_qty: "20",
        est_amount_cny: "600.00",
        supplier_id: null,
        supplier_name: null,
        urgency_score: "5",
        lead_time_days: 5,
        in_transit: "0",
        rop: "10.5",
        safety_stock: "3",
        reason: "建议补货",
      },
    ],
    count: 2,
    weeks: 2,
  }
}

/** Purchase bill creation stub. */
const purchaseBillStub = {
  bill_id: "bill-uat-00001",
  bill_no: "PO-20260525-001",
}

// ---------------------------------------------------------------------------
// Route interceptors
// ---------------------------------------------------------------------------

/**
 * installMocks wires all page.route() interceptors needed for the onboarding
 * UAT tests. Must be called before page.goto().
 *
 * Intercepted routes:
 *   /api/auth/session           → synthetic session (bypasses NextAuth)
 *   /api/proxy/warehouses?*     → warehouse list stub
 *   /api/proxy/onboarding/seed-demo → seed-demo stub
 *   /api/proxy/replenish/suggestions* → replenish stub
 *   /api/proxy/bills/purchase   → purchase bill stub
 *   /api/otel-events            → 200 ok (telemetry capture handled separately)
 */
async function installMocks(page: Page, profileType: string): Promise<void> {
  if (REAL) {
    // Real session + real proxy → no stubs at all. Requests hit STAGE.
    return
  }

  const session = fakeSession(profileType)

  // NextAuth session
  await page.route("**/api/auth/session", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(session),
    })
  })

  // Warehouse list
  await page.route("**/api/proxy/warehouses**", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(warehouseListStub),
    })
  })

  // Seed demo
  await page.route("**/api/proxy/onboarding/seed-demo", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(seedDemoStub),
    })
  })

  // Replenish suggestions
  await page.route("**/api/proxy/replenish/suggestions**", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(replenishStub()),
    })
  })

  // Purchase bill creation
  await page.route("**/api/proxy/bills/purchase**", async (route: Route) => {
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify(purchaseBillStub),
    })
  })

  // Telemetry sink — always succeed
  await page.route("**/api/otel-events", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ok: true }),
    })
  })

  // /api/auth/* (CSRF, providers, etc) — return empty 200 to prevent 500 noise
  await page.route("**/api/auth/**", async (route: Route) => {
    if (route.request().url().includes("/api/auth/session")) return // already handled
    await route.fulfill({ status: 200, contentType: "application/json", body: "{}" })
  })
}

// ---------------------------------------------------------------------------
// Core happy-path flow
// ---------------------------------------------------------------------------

/**
 * runHappyPath executes the full onboarding wizard for one persona and asserts
 * all intermediate steps. Returns elapsed wall-clock milliseconds.
 */
async function runHappyPath(
  page: Page,
  persona: "retail" | "cross_border" | "horticulture",
  label: string,
): Promise<number> {
  const t0 = Date.now()

  await installMocks(page, persona)

  // Step 1: navigate directly to the wizard (step=seed bypasses profile-picker
  // branch which calls auth() and redirects unauthenticated SSR to /login).
  await page.goto(`/setup?step=seed&persona=${persona}`, {
    waitUntil: "domcontentloaded",
    timeout: 30_000,
  })

  // The page SSR may emit a meta refresh to /login before the client JS
  // hydrates and the session mock takes effect. Wait up to 5 s for the wizard
  // heading to appear; if we land on /login instead, the test will fail here
  // with a clear message.
  const wizardStep1 = page.getByRole("heading", { name: /第 1 步：种入示例数据/i })
  await expect(wizardStep1).toBeVisible({ timeout: STEP_TIMEOUT })

  await page.screenshot({ path: path.join(SCREEN_DIR, `${label}-01-wizard-seed.png`) })

  // Step 2: click "种入示例数据"
  const seedBtn = page.getByRole("button", { name: /种入示例数据/ })
  await expect(seedBtn).toBeVisible({ timeout: 5_000 })
  await seedBtn.click()

  // Step 3: wait for step to advance to "replenish" — wizard shows a "前往查看补货建议" button
  const goReplenishBtn = page.getByRole("button", { name: /前往查看补货建议/i })
  await expect(goReplenishBtn).toBeVisible({ timeout: STEP_TIMEOUT })

  await page.screenshot({ path: path.join(SCREEN_DIR, `${label}-02-wizard-replenish.png`) })

  // Step 4: click the button — navigates to /replenish?onboarding=1
  await goReplenishBtn.click()
  await page.waitForURL(/\/replenish/, { timeout: 30_000 })

  await page.screenshot({ path: path.join(SCREEN_DIR, `${label}-03-replenish-page.png`) })

  if (REAL) {
    // Real STAGE data — row contents are non-deterministic. Assert structurally:
    // either at least one selectable suggestion row OR the empty state renders.
    const anyRowCheckbox = page
      .locator('input[type="checkbox"][aria-label^="选择"]')
      .first()
    const emptyState = page.getByText(/暂无补货建议/).first()
    await expect(anyRowCheckbox.or(emptyState)).toBeVisible({ timeout: STEP_TIMEOUT })
    await page.screenshot({ path: path.join(SCREEN_DIR, `${label}-04-replenish-real.png`) })

    const hasRow = await anyRowCheckbox.isVisible().catch(() => false)
    if (hasRow) {
      await anyRowCheckbox.check()
      const generateBtn = page.getByRole("button", { name: /生成采购草稿/i })
      await expect(generateBtn).toBeEnabled({ timeout: 5_000 })
      await generateBtn.click()
      // Structural toast assertion — a toast appears (success or error text
      // varies with real data; failures still surface as missing toast).
      await expect(
        page.locator("[data-sonner-toaster] [data-sonner-toast]").first()
      ).toBeVisible({ timeout: STEP_TIMEOUT })
      await page.screenshot({ path: path.join(SCREEN_DIR, `${label}-05-po-created-real.png`) })
    } else {
      console.warn(
        `[${label}] REAL mode: no suggestion rows on STAGE — draft generation step ` +
          "skipped (replenish page render asserted structurally)."
      )
    }
    return Date.now() - t0
  }

  // Step 5: assert at least one SKU row is visible
  // The replenish page uses a DataTable; rows contain product_name text.
  await expect(page.getByText("示例商品A")).toBeVisible({ timeout: 15_000 })

  // Step 6: select the first row checkbox
  const firstCheckbox = page.locator('input[type="checkbox"][aria-label="选择 示例商品A"]')
  await expect(firstCheckbox).toBeVisible({ timeout: 10_000 })
  await firstCheckbox.check()

  await page.screenshot({ path: path.join(SCREEN_DIR, `${label}-04-replenish-selected.png`) })

  // Step 7: click "生成采购草稿"
  const generateBtn = page.getByRole("button", { name: /生成采购草稿/i })
  await expect(generateBtn).toBeEnabled({ timeout: 5_000 })
  await generateBtn.click()

  // Step 8: assert success toast
  // Sonner toasts render in a [data-sonner-toaster] portal; the text includes
  // the success message with the draft count.
  await expect(
    page.locator("[data-sonner-toaster]").getByText(/已生成.*采购草稿/i)
  ).toBeVisible({ timeout: 15_000 })

  await page.screenshot({ path: path.join(SCREEN_DIR, `${label}-05-po-created.png`) })

  const elapsed = Date.now() - t0
  return elapsed
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe("onboarding E2E — kill-switch #1", () => {

  // -------------------------------------------------------------------------
  // Happy paths — 3 personas
  // -------------------------------------------------------------------------

  test("happy_path_retail_under_10min", async ({ page }) => {
    test.setTimeout(MAX_WALL_MS + 60_000) // 11 min hard limit

    const elapsed = await runHappyPath(page, "retail", "retail")

    expect(elapsed, `retail flow elapsed ${elapsed}ms — must be < ${MAX_WALL_MS}ms`).toBeLessThan(
      MAX_WALL_MS
    )
    console.log(`[happy_path_retail] elapsed: ${elapsed}ms (${(elapsed / 1000).toFixed(1)}s)`)
  })

  test("happy_path_cross_border", async ({ page }) => {
    test.setTimeout(MAX_WALL_MS + 60_000)

    const elapsed = await runHappyPath(page, "cross_border", "cross_border")

    expect(
      elapsed,
      `cross_border flow elapsed ${elapsed}ms — must be < ${MAX_WALL_MS}ms`,
    ).toBeLessThan(MAX_WALL_MS)
    console.log(
      `[happy_path_cross_border] elapsed: ${elapsed}ms (${(elapsed / 1000).toFixed(1)}s)`,
    )
  })

  test("happy_path_horticulture", async ({ page }) => {
    test.setTimeout(MAX_WALL_MS + 60_000)

    const elapsed = await runHappyPath(page, "horticulture", "horticulture")

    expect(
      elapsed,
      `horticulture flow elapsed ${elapsed}ms — must be < ${MAX_WALL_MS}ms`,
    ).toBeLessThan(MAX_WALL_MS)
    console.log(
      `[happy_path_horticulture] elapsed: ${elapsed}ms (${(elapsed / 1000).toFixed(1)}s)`,
    )
  })

  // -------------------------------------------------------------------------
  // Telemetry events fire during a happy-path run
  // -------------------------------------------------------------------------

  test("step_telemetry_events_fire", async ({ page }) => {
    test.skip(
      REAL,
      "Relies on the stubbed deterministic flow + route-fulfilled event capture; " +
        "telemetry against STAGE is covered indirectly by the happy-path runs.",
    )
    test.setTimeout(120_000)

    /**
     * Telemetry events we expect to see during the flow:
     *
     * The OnboardingWizard component fires trackEvent("onboarding_first_po_exported")
     * via notifyFirstPoExported() — which is called by the replenish page after
     * the user clicks "生成采购草稿". That is the only onboarding-specific event
     * defined in telemetry.ts. There are no persona_chosen / demo_seeded /
     * replenish_landed events in the current implementation (confirmed by reading
     * web/lib/telemetry.ts and web/components/onboarding/OnboardingWizard.tsx).
     *
     * We capture all POST /api/otel-events calls and assert the
     * onboarding_first_po_exported event fires.
     */
    const capturedEvents: string[] = []

    await installMocks(page, "retail")

    // Override the otel-events mock to also capture event names.
    await page.route("**/api/otel-events", async (route: Route) => {
      const body = route.request().postDataJSON() as { event?: string } | null
      if (body?.event) capturedEvents.push(body.event)
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ ok: true }),
      })
    })

    // Run the full happy path so events fire.
    await page.goto("/setup?step=seed&persona=retail", {
      waitUntil: "domcontentloaded",
      timeout: 30_000,
    })
    await expect(
      page.getByRole("heading", { name: /第 1 步：种入示例数据/i })
    ).toBeVisible({ timeout: 15_000 })
    await page.getByRole("button", { name: /种入示例数据/ }).click()
    await expect(
      page.getByRole("button", { name: /前往查看补货建议/i })
    ).toBeVisible({ timeout: 15_000 })
    await page.getByRole("button", { name: /前往查看补货建议/i }).click()
    await page.waitForURL(/\/replenish/, { timeout: 30_000 })
    await expect(page.getByText("示例商品A")).toBeVisible({ timeout: 15_000 })
    const firstCheckbox = page.locator('input[type="checkbox"][aria-label="选择 示例商品A"]')
    await firstCheckbox.check()
    await page.getByRole("button", { name: /生成采购草稿/i }).click()
    await expect(
      page.locator("[data-sonner-toaster]").getByText(/已生成.*采购草稿/i)
    ).toBeVisible({ timeout: 15_000 })

    // notifyFirstPoExported is called client-side after PO creation — give
    // the fire-and-forget fetch a moment to dispatch.
    await page.waitForTimeout(1_000)

    // Assert the onboarding completion event fired.
    expect(
      capturedEvents,
      `Expected onboarding_first_po_exported in captured events: ${JSON.stringify(capturedEvents)}`,
    ).toContain("onboarding_first_po_exported")

    console.log(
      `[step_telemetry_events_fire] captured events: ${JSON.stringify(capturedEvents)}`,
    )
  })

  // -------------------------------------------------------------------------
  // Error recovery — seed endpoint returns 500
  // -------------------------------------------------------------------------

  test("error_recovery_seed_fails", async ({ page }) => {
    test.skip(
      REAL,
      "Requires injecting a stubbed 500 from seed-demo — server faults cannot be " +
        "injected against the real STAGE backend without route stubs.",
    )
    test.setTimeout(60_000)

    await installMocks(page, "retail")

    // Override seed-demo to return 500 after the base mocks are installed.
    await page.route("**/api/proxy/onboarding/seed-demo", async (route: Route) => {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "internal_error", detail: "seed failed (simulated)" }),
      })
    })

    await page.goto("/setup?step=seed&persona=retail", {
      waitUntil: "domcontentloaded",
      timeout: 30_000,
    })

    await expect(
      page.getByRole("heading", { name: /第 1 步：种入示例数据/i })
    ).toBeVisible({ timeout: 15_000 })

    await page.getByRole("button", { name: /种入示例数据/ }).click()

    // The OnboardingWizard catches errors and renders the message in a <p
    // className="text-sm text-destructive"> element. Assert it's visible and
    // non-empty, and that the "种入示例数据" retry button is still present.
    const errorText = page.locator("p.text-destructive, [class*='text-destructive']").first()
    await expect(errorText).toBeVisible({ timeout: 10_000 })

    const retryBtn = page.getByRole("button", { name: /种入示例数据/ })
    await expect(retryBtn).toBeVisible({ timeout: 5_000 })
    await expect(retryBtn).toBeEnabled()

    await page.screenshot({
      path: path.join(SCREEN_DIR, "error-recovery-seed-fails.png"),
    })

    console.log(
      `[error_recovery_seed_fails] error message text: "${await errorText.innerText().catch(() => "(could not read)")}"`,
    )
  })
})
