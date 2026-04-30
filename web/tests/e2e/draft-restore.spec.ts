import { test, expect } from "@playwright/test"

/**
 * E2E: IndexedDB draft restore on new-product form.
 *
 * These tests require a running STAGE environment.
 * Run with: bunx playwright test tests/e2e/draft-restore.spec.ts
 *
 * All tests are skipped in local CI — enable on STAGE by removing test.skip.
 */

test.skip("draft-restore: fill form, navigate away, return — draft is restored", async ({
  page,
}) => {
  await page.goto("/products/new")

  // Fill in product name
  await page.getByLabel("商品名称").fill("测试草稿商品")
  await page.getByLabel("商品编码").fill("DRAFT-001")

  // Navigate away to trigger IDB save (debounce 500ms)
  await page.waitForTimeout(600)
  await page.goto("/products")

  // Navigate back
  await page.goto("/products/new")

  // Expect draft banner
  await expect(page.getByText("已恢复")).toBeVisible({ timeout: 3_000 })

  // Expect form values restored
  await expect(page.getByLabel("商品名称")).toHaveValue("测试草稿商品")
  await expect(page.getByLabel("商品编码")).toHaveValue("DRAFT-001")
})

test.skip("draft-restore: discard button clears the draft", async ({ page }) => {
  await page.goto("/products/new")

  await page.getByLabel("商品名称").fill("丢弃测试")
  await page.waitForTimeout(600)
  await page.goto("/products")
  await page.goto("/products/new")

  await expect(page.getByText("已恢复")).toBeVisible({ timeout: 3_000 })

  // Click discard
  await page.getByRole("button", { name: "放弃" }).click()

  // Banner should disappear
  await expect(page.getByText("已恢复")).not.toBeVisible()
  // Field should be cleared
  await expect(page.getByLabel("商品名称")).toHaveValue("")
})
