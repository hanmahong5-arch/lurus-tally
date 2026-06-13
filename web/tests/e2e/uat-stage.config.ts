/**
 * Playwright config — UAT specs against the REAL STAGE backend.
 *
 * Topology: Playwright starts a LOCAL `next dev` on port 3030 whose
 * BACKEND_URL points at https://tally-stage.lurus.cn. The STAGE tally-web pod
 * is never touched. The dev Credentials provider (AUTH_DEV_PROVIDER=true,
 * non-production only — see devProviderEnabled() in web/auth.ts) carries the
 * STAGE PAT into the session so /api/proxy/* forwards real Bearer requests.
 *
 * Projects:
 *   setup              — REST login + storageState (uat-stage.setup.ts)
 *   chromium-uat-stage — the three UAT specs in REAL mode (UAT_REAL=1)
 */
import { defineConfig, devices } from "@playwright/test"
import crypto from "crypto"
import path from "path"

// UAT_REAL=1 tells the specs to skip route stubs and relax data-dependent
// assertions. Set here so the runner and its forked workers both inherit it.
process.env.UAT_REAL = "1"

const WEB_DIR = path.resolve(__dirname, "../..")
const STATE_FILE = path.join(WEB_DIR, "test-results-uat", "uat-stage-state.json")

export default defineConfig({
  testDir: ".",
  fullyParallel: false,
  workers: 1,
  reporter: [
    ["list"],
    ["html", { open: "never", outputFolder: path.join(WEB_DIR, "playwright-report-uat-stage") }],
  ],
  outputDir: path.join(WEB_DIR, "test-results-uat", "stage"),
  timeout: 120_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: "http://localhost:3030",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    ignoreHTTPSErrors: true,
  },
  webServer: {
    // package.json "dev" = `next dev`; extra args are forwarded to next.
    command: "bun run dev --port 3030",
    cwd: WEB_DIR,
    url: "http://localhost:3030",
    reuseExistingServer: true,
    // Generous budget for Next.js dev cold start (first compile).
    timeout: 240_000,
    env: {
      AUTH_DEV_PROVIDER: "true",
      BACKEND_URL: "https://tally-stage.lurus.cn",
      // Any secret works — sessions only need to survive this run. A caller-
      // provided AUTH_SECRET wins so a reused server stays cookie-compatible.
      AUTH_SECRET: process.env.AUTH_SECRET ?? crypto.randomBytes(32).toString("hex"),
      NEXTAUTH_URL: "http://localhost:3030",
    },
  },
  projects: [
    {
      name: "setup",
      testMatch: /uat-stage\.setup\.ts/,
    },
    {
      name: "chromium-uat-stage",
      dependencies: ["setup"],
      testMatch: /uat-(palette|onboarding|decisions)\.spec\.ts/,
      use: {
        ...devices["Desktop Chrome"],
        storageState: STATE_FILE,
      },
    },
  ],
})
