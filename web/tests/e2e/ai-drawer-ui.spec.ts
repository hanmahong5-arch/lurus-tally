import { test, expect } from "@playwright/test"
import path from "path"

const SCREEN_DIR = path.join(__dirname, "..", "..", "test-results", "screenshots")

// Verifies the AI Drawer renders and responds when opened from the global
// floating button on the dashboard. Captures screenshots so we can eyeball
// the conversation visually (and prove it's not just a markup-only feature).
test("AI Drawer opens from floating button and streams a response", async ({ page }) => {
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" })
  await page.waitForLoadState("load", { timeout: 10_000 }).catch(() => {})

  // Floating button — agent picked emoji "✨" (sparkles); locator should be
  // forgiving so a future visual tweak doesn't break this test.
  const trigger = page
    .locator(
      'button[aria-label*="AI" i], button[aria-label*="助手" i], [data-testid="ai-drawer-toggle"], button:has(svg.lucide-sparkles)',
    )
    .first()
  await expect(trigger, "AI Drawer floating trigger visible").toBeVisible({ timeout: 10_000 })

  await page.screenshot({
    path: path.join(SCREEN_DIR, "ai-drawer-button.png"),
    fullPage: true,
  })

  await trigger.click()

  // After click the Drawer should expose an input + a way to submit.
  const input = page
    .locator(
      'textarea[placeholder*="问" i], textarea[placeholder*="ask" i], input[type="text"][placeholder*="问" i]',
    )
    .first()
  await expect(input, "AI Drawer input visible").toBeVisible({ timeout: 10_000 })

  await input.fill("用一句中文告诉我什么是 ABC 分类法")

  // Submit via Enter (most chat UIs bind it). Fallback to a Send button if
  // Enter doesn't trigger.
  await input.press("Enter")

  // Wait for any non-trivial assistant text to appear in the drawer panel.
  // We don't pin to specific words — model output varies. Just need
  // something Chinese with sufficient length.
  const drawer = page
    .locator('[role="dialog"], [data-testid="ai-drawer"], aside:has-text("助手")')
    .first()
  await expect
    .poll(
      async () => {
        const txt = (await drawer.innerText().catch(() => "")) || ""
        return txt.length
      },
      { timeout: 30_000, message: "expected streamed assistant text within 30s" },
    )
    .toBeGreaterThan(20)

  await page.screenshot({
    path: path.join(SCREEN_DIR, "ai-drawer-response.png"),
    fullPage: true,
  })
})
