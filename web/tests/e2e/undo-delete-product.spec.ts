import { test, expect } from "@playwright/test"

/**
 * E2E: Undo-aware product delete via Cmd+Z.
 *
 * Requires a running STAGE environment with at least one product in the catalogue.
 * Run with: bunx playwright test tests/e2e/undo-delete-product.spec.ts
 *
 * All tests are skipped in local CI — enable on STAGE by removing test.skip.
 */

test.skip("undo-delete-product: delete then Cmd+Z restores the product", async ({
  page,
  isMobile,
}) => {
  if (isMobile) test.skip()

  await page.goto("/products")

  // Wait for at least one product row
  const deleteBtn = page.getByRole("button", { name: "删除" }).first()
  await expect(deleteBtn).toBeVisible()

  // Capture the product name before deletion
  const row = deleteBtn.locator("..").locator("..")
  const productName = await row.getByRole("cell").nth(1).innerText()

  // Delete the product
  await deleteBtn.click()

  // Wait for the row to disappear
  await expect(page.getByText(productName)).not.toBeVisible({ timeout: 3_000 })

  // Press Cmd+Z to undo
  await page.keyboard.press("Meta+z")

  // Wait for "已撤销" toast
  await expect(page.getByText("已撤销")).toBeVisible({ timeout: 3_000 })

  // Product should reappear within 3 seconds (revert + list reload)
  await expect(page.getByText(productName)).toBeVisible({ timeout: 3_000 })
})
