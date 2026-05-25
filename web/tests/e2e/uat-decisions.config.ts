/**
 * Playwright config for the UAT decision surfaces test suite.
 *
 * Targets the locally running stack:
 *   FE  — http://localhost:3030
 *   API — http://localhost:18200 (no auth middleware; X-Tenant-ID header trusted)
 *
 * No auth setup dependency: the backend is started with ZitadelDomain unset, so
 * requests carrying X-Tenant-ID are accepted directly.
 */
import { defineConfig, devices } from "@playwright/test"
import path from "path"

export default defineConfig({
  testDir: path.join(__dirname),
  testMatch: /uat-decisions\.spec\.ts/,
  fullyParallel: false,
  workers: 1,
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report-uat" }]],
  outputDir: "test-results-uat",
  timeout: 90_000,
  expect: { timeout: 15_000 },
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
      use: { ...devices["Desktop Chrome"] },
    },
  ],
})
