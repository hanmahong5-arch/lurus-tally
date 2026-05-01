// web/tests/e2e/projects.spec.ts
// Smoke tests that can run without seed data.
// Data-dependent tests (create/search) are skipped pending test backend.
// Run: bunx playwright test projects.spec.ts

import { test, expect } from "@playwright/test"

test("projects page title is visible", async ({ page }) => {
  await page.goto("/projects")
  await expect(
    page.locator("h1, h2, [data-testid='page-title']")
  ).toContainText(["项目"])
})

test("new project button is present", async ({ page }) => {
  await page.goto("/projects")
  await expect(page.locator("button", { hasText: "新建项目" })).toBeVisible()
})

test.skip("create project and card appears in grid", async ({ page }) => {
  // Skipped: requires authenticated session with seeded tenant.
  // TODO: enable when test backend has E2E auth setup.
  await page.goto("/projects")
  await page.click("button:has-text('新建项目')")
  await page.fill("[name='name']", "E2E工程项目")
  await page.fill("[name='code']", "E2E-001")
  await page.click("button[type='submit']")
  await expect(page.locator("[data-testid='project-card']")).toContainText(
    "E2E工程项目"
  )
})
