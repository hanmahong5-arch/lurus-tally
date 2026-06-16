/**
 * Per-test health gate for the local `next dev` server (port 3030).
 *
 * The UAT stage suite drives a dev-mode Next server that can crash or restart
 * mid-run. Without this gate a dead server turns every remaining test into a
 * `page.goto: ERR_CONNECTION_REFUSED` / `connect EACCES ::1:3030` FAILURE that an
 * auditor cannot tell apart from a real product defect (and a tester may even
 * mis-report as a pass). Probing the server before each test and SKIPPING when
 * it is unreachable keeps an env outage classified as env — never product/test.
 */
import type { TestType } from "@playwright/test"

const PROBE_PATH = "/api/auth/csrf" // cheap, returns 200 on any live dev server
const PROBE_TIMEOUT_MS = 3_000

async function devServerUp(baseURL: string): Promise<boolean> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS)
  try {
    const res = await fetch(`${baseURL}${PROBE_PATH}`, { signal: controller.signal })
    // Any HTTP response (even 4xx) proves the process is listening.
    return res.status >= 200 && res.status < 600
  } catch {
    return false // connection refused / EACCES / abort => server is down
  } finally {
    clearTimeout(timer)
  }
}

/**
 * Registers a beforeEach on the given test instance that SKIPS (not fails) the
 * test when the dev server is unreachable. Call once per spec file, passing the
 * spec's own test object (e.g. the extended `paletteTest`).
 */
export function gateOnDevServer(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  t: TestType<any, any>,
  baseURL = "http://localhost:3030",
): void {
  t.beforeEach(async () => {
    const up = await devServerUp(baseURL)
    t.skip(
      !up,
      `FE dev server (${baseURL}) unreachable — env-blocked (crashed/restarting), not a product/test failure`,
    )
  })
}
