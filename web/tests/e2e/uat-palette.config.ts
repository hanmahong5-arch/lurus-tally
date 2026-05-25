import { defineConfig, devices } from "@playwright/test"

/**
 * UAT config for Command Palette E2E tests.
 *
 * Targets a locally running dev stack:
 *   Frontend: http://localhost:3030  (Next.js dev)
 *   Backend:  http://localhost:18200 (Go server, auth disabled)
 *
 * No auth setup project — backend UAT mode skips Zitadel middleware.
 * No storageState — palette tests hit the backend directly via the
 *   `request` fixture's newContext, bypassing the FE proxy auth check.
 */
export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  workers: 1,
  reporter: [
    ["list"],
    ["html", { open: "never", outputFolder: "playwright-report-uat" }],
  ],
  outputDir: "test-results-uat",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: "http://localhost:3030",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: "chromium-uat",
      testMatch: /uat-palette\.spec\.ts/,
      use: { ...devices["Desktop Chrome"] },
      // No dependencies — no storageState — no setup project.
    },
  ],
})
