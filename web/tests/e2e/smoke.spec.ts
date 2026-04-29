import { test, expect, type Page } from "@playwright/test"
import path from "path"
import fs from "fs"

const SCREEN_DIR = path.join(__dirname, "..", "..", "test-results", "screenshots")
fs.mkdirSync(SCREEN_DIR, { recursive: true })

const PAGES = [
  { path: "/dashboard", expectVisible: /Lurus Tally|欢迎/i, label: "dashboard" },
  { path: "/products", expectVisible: /商品管理|新增商品|product/i, label: "products" },
  { path: "/purchases", expectVisible: /采购|新增采购/i, label: "purchases" },
  { path: "/sales", expectVisible: /销售|新增销售/i, label: "sales" },
  { path: "/finance/exchange-rates", expectVisible: /汇率|币种|exchange/i, label: "finance" },
  { path: "/subscription", expectVisible: /订阅与计费|套餐/i, label: "subscription" },
] as const

function attachConsoleCollector(page: Page) {
  const errors: string[] = []
  page.on("console", (msg) => {
    if (msg.type() === "error") errors.push(`[console.error] ${msg.text()}`)
  })
  page.on("pageerror", (err) => errors.push(`[pageerror] ${err.message}`))
  return errors
}

for (const { path: routePath, expectVisible, label } of PAGES) {
  test(`${routePath} renders without errors`, async ({ page }) => {
    const consoleErrors = attachConsoleCollector(page)
    const failedRequests: string[] = []
    page.on("requestfailed", (req) => {
      const url = req.url()
      if (!url.includes("favicon") && !url.includes("hot-update")) {
        failedRequests.push(`${req.method()} ${url} — ${req.failure()?.errorText}`)
      }
    })

    const resp = await page.goto(routePath, { waitUntil: "domcontentloaded" })
    await page.waitForLoadState("load", { timeout: 10_000 }).catch(() => {})

    expect(resp?.status(), `HTTP status for ${routePath}`).toBeLessThan(400)

    // Take a screenshot for visual review
    await page.screenshot({ path: path.join(SCREEN_DIR, `${label}.png`), fullPage: true })

    // Check expected content is actually visible (not raw HTML — visible text)
    const bodyText = (await page.locator("body").innerText()).slice(0, 4000)
    expect(bodyText, `visible text on ${routePath}`).toMatch(expectVisible)

    // Page must NOT show Next.js 404 page (the visible "404 / This page could not be found")
    const has404Heading = await page
      .getByRole("heading", { name: "404" })
      .isVisible()
      .catch(() => false)
    expect(has404Heading, `${routePath} shows 404 heading`).toBe(false)

    // No application crash overlays
    expect(bodyText).not.toMatch(/Application error|Internal Server Error/i)

    // Critical errors only — filter known noise
    const critical = consoleErrors.filter(
      (e) => !e.match(/favicon|hydrat|chunk|preload|404|Failed to load resource/i),
    )
    expect(critical, `console errors on ${routePath}`).toEqual([])

    const criticalNetwork = failedRequests.filter(
      (r) => !r.includes("favicon") && !r.includes("/_next/data"),
    )
    expect(criticalNetwork, `failed requests on ${routePath}`).toEqual([])
  })
}

test("sidebar shows POS link for retail profile", async ({ page }) => {
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" })
  const posLink = page.getByRole("link", { name: /POS/i })
  await expect(posLink).toBeVisible({ timeout: 10_000 })
})

test("session has tenantId and onboarded state", async ({ request }) => {
  const sessionRes = await request.get("/api/auth/session")
  expect(sessionRes.ok()).toBe(true)
  const session = await sessionRes.json()
  expect(session.user, "session.user").toBeTruthy()
  expect(session.user.tenantId, "tenantId set").toBeTruthy()
  expect(session.user.profileType).toMatch(/retail|cross_border/)
  expect(session.user.isFirstTime).toBe(false)
})

test("backend /api/v1/me via session token", async ({ request }) => {
  const sessionRes = await request.get("/api/auth/session")
  const session = await sessionRes.json()
  expect(session.accessToken).toBeTruthy()

  const meRes = await request.get("/api/v1/me", {
    headers: { Authorization: `Bearer ${session.accessToken}` },
  })
  expect(meRes.ok(), "GET /api/v1/me").toBe(true)
  const me = await meRes.json()
  expect(me.tenant_id).toBeTruthy()
  expect(me.is_first_time).toBe(false)
  expect(me.profile_type).toMatch(/retail|cross_border/)
})
