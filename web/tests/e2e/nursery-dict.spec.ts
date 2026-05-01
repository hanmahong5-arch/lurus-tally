/**
 * Playwright E2E spec — Nursery Dictionary page (Story 28.1).
 *
 * Requirements:
 *   - Backend running with migration 000028 applied
 *   - SEED_NURSERY_DICT=true to have 200 seed species loaded
 *   - Auth session established via auth.setup.ts
 *
 * Run: bunx playwright test nursery-dict.spec.ts
 *
 * NOTE: All tests are marked test.skip in CI because seed data is OFF by default.
 * Remove the skip annotation when running against a STAGE environment with seed loaded.
 */
import { test, expect } from "@playwright/test"

test.describe("Nursery Dictionary — /dictionary", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/dictionary")
    // Wait for the page header to be visible before assertions
    await page.waitForSelector("h1", { timeout: 10000 })
  })

  test.skip(
    "dictionary list page shows 200 seed species (requires SEED_NURSERY_DICT=true)",
    async ({ page }) => {
      // Table should show first page (20 rows) with default page size
      const rows = page.locator("tbody tr")
      await expect(rows).toHaveCount(20)
      // Total count label shows >= 200
      await expect(page.locator('[data-testid="total-count"]')).toContainText("200")
    }
  )

  test.skip(
    "search for 红枫 returns at least 1 result (requires seed data)",
    async ({ page }) => {
      await page.fill('[placeholder*="搜索"]', "红枫")
      await page.waitForTimeout(400) // debounce
      const rows = page.locator("tbody tr")
      const count = await rows.count()
      expect(count).toBeGreaterThan(0)
      await expect(rows.first()).toContainText("红枫")
    }
  )

  test("page header 苗木字典 is visible", async ({ page }) => {
    await expect(page.locator("h1")).toContainText("苗木字典")
  })

  test.skip(
    "click row opens drawer with latin name (requires seed data)",
    async ({ page }) => {
      await page.locator("tbody tr").first().click()
      await expect(
        page.locator('[data-testid="nursery-detail-drawer"]')
      ).toBeVisible()
      await expect(page.locator('[data-testid="latin-name"]')).not.toBeEmpty()
    }
  )

  test.skip(
    "create new entry and it appears in list",
    async ({ page }) => {
      await page.click('button:has-text("新增苗木")')
      await page.fill('[name="name"]', "E2E测试苗木")
      await page.selectOption('[name="type"]', "shrub")
      await page.click('button[type="submit"]')
      await expect(page.locator("tbody")).toContainText("E2E测试苗木")
    }
  )

  test("+ 新增苗木 button is visible", async ({ page }) => {
    await expect(page.locator('button:has-text("新增苗木")')).toBeVisible()
  })

  test("search input is present", async ({ page }) => {
    await expect(
      page.locator('[placeholder*="搜索苗木名称"]')
    ).toBeVisible()
  })
})
