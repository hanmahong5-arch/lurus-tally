import { test, expect } from "@playwright/test"

/**
 * E2E: Undo stack depth (max 10) and 30s expiry.
 *
 * Requires a running STAGE environment with at least 11 products.
 * Run with: bunx playwright test tests/e2e/undo-stack-depth.spec.ts
 *
 * All tests are skipped in local CI — enable on STAGE by removing test.skip.
 * The expiry test is marked test.slow() because it waits 31 seconds.
 */

test.skip("undo-stack-depth: only 10 undos available after 11 deletes", async ({
  page,
  isMobile,
}) => {
  if (isMobile) test.skip()

  await page.goto("/products")

  // Delete 11 products in sequence
  for (let i = 0; i < 11; i++) {
    const deleteBtn = page.getByRole("button", { name: "删除" }).first()
    await expect(deleteBtn).toBeVisible({ timeout: 3_000 })
    await deleteBtn.click()
    await page.waitForTimeout(200)
  }

  // Undo 10 times — should succeed
  for (let i = 0; i < 10; i++) {
    await page.keyboard.press("Meta+z")
    await expect(page.getByText("已撤销")).toBeVisible({ timeout: 3_000 })
    await page.waitForTimeout(100)
  }

  // 11th Cmd+Z — stack is empty
  await page.keyboard.press("Meta+z")
  await expect(page.getByText("没有可撤销的操作")).toBeVisible({ timeout: 3_000 })
})

test.slow()
test.skip("undo-stack-depth: entry expires after 30s", async ({ page, isMobile }) => {
  if (isMobile) test.skip()

  await page.goto("/products")

  const deleteBtn = page.getByRole("button", { name: "删除" }).first()
  await expect(deleteBtn).toBeVisible()
  await deleteBtn.click()

  // Wait 31 seconds for the entry to expire
  await page.waitForTimeout(31_000)

  await page.keyboard.press("Meta+z")
  await expect(page.getByText("没有可撤销的操作")).toBeVisible({ timeout: 3_000 })
})
