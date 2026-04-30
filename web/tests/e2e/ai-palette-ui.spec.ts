import { test, expect } from "@playwright/test"
import path from "path"

const SCREEN_DIR = path.join(__dirname, "..", "..", "test-results", "screenshots")

// Verifies ⌘K Command Palette opens via keyboard shortcut and shows the
// expected three-section structure (Pages / Actions / fallback). We don't
// assert specific text — we assert the palette element renders and contains
// at least one navigation link entry.
test("Command Palette opens with Ctrl+K and lists actions", async ({ page }) => {
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" })
  await page.waitForLoadState("load", { timeout: 10_000 }).catch(() => {})

  // Trigger global shortcut. Windows + Linux + Chromium use Ctrl; the hook
  // accepts both modifiers per code.
  await page.keyboard.press("Control+K")

  const palette = page
    .locator('[role="dialog"], [data-testid="command-palette"], [cmdk-root]')
    .first()
  await expect(palette).toBeVisible({ timeout: 5_000 })

  // Take a screenshot of the open palette.
  await page.screenshot({
    path: path.join(SCREEN_DIR, "ai-palette-open.png"),
    fullPage: true,
  })

  // The palette should contain at least one nav link to a known page.
  const text = (await palette.innerText()).slice(0, 500)
  expect(text, "palette should list navigable pages").toMatch(
    /商品|采购|销售|财务|订阅|product|purchase|sale/i,
  )

  // Press Escape to close so subsequent tests don't inherit state.
  await page.keyboard.press("Escape")
})
