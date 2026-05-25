/**
 * Playwright config for onboarding UAT.
 *
 * Targets the local UAT stack:
 *   FE  → http://localhost:3030
 *   BE  → http://localhost:18200 (auth middleware disabled — ZITADEL_DOMAIN unset)
 *
 * Auth strategy (path B — fake session cookie):
 *   The Next.js middleware and /setup page call auth() server-side; the
 *   /api/auth/session endpoint returns 500 in the UAT env because NEXTAUTH_URL
 *   and AUTH_SECRET are not configured. Rather than fight the NextAuth JWE
 *   layer, every test that navigates a protected route intercepts the Next.js
 *   session fetch itself via page.route() and returns a synthetic session JSON.
 *   The /api/proxy/* calls are similarly intercepted and fulfilled with
 *   stub responses so the tests do not depend on backend tenant auth.
 *
 * No setup project / no storageState dependency.
 */
import { defineConfig, devices } from "@playwright/test"

export default defineConfig({
  testDir: ".",
  testMatch: "uat-onboarding.spec.ts",
  fullyParallel: false,
  workers: 1,
  reporter: [
    ["list"],
    ["html", { open: "never", outputFolder: "../../playwright-report/uat-onboarding" }],
  ],
  outputDir: "../../test-results/uat-onboarding",
  // 10 minutes hard wall-clock budget for the whole spec; individual test
  // timeouts are set per-test below.
  timeout: 660_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: "http://localhost:3030",
    trace: "retain-on-failure",
    screenshot: "on",
    video: "retain-on-failure",
    ignoreHTTPSErrors: true,
    // Disable service workers so they don't interfere with page.route() mocks.
    serviceWorkers: "block",
  },
  projects: [
    {
      name: "uat-onboarding",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
})
