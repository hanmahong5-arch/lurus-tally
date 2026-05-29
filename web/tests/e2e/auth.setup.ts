import { test, expect } from "@playwright/test"
import path from "path"
import fs from "fs"

const AUTH_FILE = path.join(__dirname, ".auth/user.json")

// Dev/E2E Credentials provider credentials.
// AUTH_DEV_PROVIDER must be set to "true" in the test environment for this to
// work. See playwright.config.ts env block and .env.example.
const DEV_EMAIL = process.env.E2E_DEV_EMAIL ?? "uat@tally.test"
const DEV_TENANT_ID = process.env.E2E_DEV_TENANT_ID ?? "00000000-0000-0000-0000-000000000001"

test("authenticate via dev credentials provider", async ({ page }) => {
  fs.mkdirSync(path.dirname(AUTH_FILE), { recursive: true })

  page.on("console", (msg) => console.log(`[browser ${msg.type()}]`, msg.text()))
  page.on("pageerror", (err) => console.log("[pageerror]", err.message))
  page.on("framenavigated", (f) => {
    if (f === page.mainFrame()) console.log("[nav]", f.url())
  })

  // 1. POST to NextAuth credentials sign-in endpoint directly.
  //    This bypasses the /login page UI and avoids the OIDC redirect entirely,
  //    which is exactly what we need for offline E2E runs (no Zitadel reachable).
  console.log("→ signing in via dev credentials provider")

  // NextAuth v5 sign-in via API route. We navigate to the credentials callback
  // URL with a POST form — NextAuth handles setting the session cookie.
  await page.goto("/api/auth/csrf")
  const csrfData = await page.evaluate(async () => {
    const res = await fetch("/api/auth/csrf")
    return (await res.json()) as { csrfToken: string }
  })
  const csrfToken = csrfData.csrfToken
  console.log("→ got csrf token")

  // POST sign-in form to NextAuth credentials endpoint.
  const signInResponse = await page.evaluate(
    async ({ email, tenantId, csrf }) => {
      const body = new URLSearchParams({
        email,
        tenantId,
        csrfToken: csrf,
        redirect: "false",
        callbackUrl: "/dashboard",
        json: "true",
      })
      const res = await fetch("/api/auth/callback/credentials", {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: body.toString(),
        credentials: "include",
      })
      return { status: res.status, url: res.url }
    },
    { email: DEV_EMAIL, tenantId: DEV_TENANT_ID, csrf: csrfToken },
  )
  console.log("→ sign-in response:", signInResponse.status, signInResponse.url)

  // 2. Navigate to dashboard — should succeed if session cookie is set.
  console.log("→ navigating to /dashboard")
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" })
  await page.waitForLoadState("networkidle", { timeout: 15_000 }).catch(() => {})

  // If middleware still redirected us (e.g., /setup for first-time users), skip.
  if (page.url().includes("/setup")) {
    console.log("→ on /setup, choosing cross_border persona")
    await page.getByRole("button", { name: "选这个" }).first().click()
    await page.waitForURL(/\/(dashboard|products)/, { timeout: 30_000 })
  }

  // 3. Persist storage state for all downstream specs.
  await page.context().storageState({ path: AUTH_FILE })
  console.log("→ auth saved:", AUTH_FILE)

  // Verify we are NOT on /login — the session must have been established.
  await expect(page).not.toHaveURL(/\/login/)
})
