import { test, expect } from "@playwright/test"
import path from "path"
import fs from "fs"

const AUTH_FILE = path.join(__dirname, ".auth/user.json")

const ZITADEL_USER = process.env.E2E_USER ?? "zitadel-admin@zitadel.auth.lurus.cn"
const ZITADEL_PASS = process.env.E2E_PASS ?? "Lurus@ops"

test("authenticate via Zitadel", async ({ page }) => {
  fs.mkdirSync(path.dirname(AUTH_FILE), { recursive: true })

  page.on("console", (msg) => console.log(`[browser ${msg.type()}]`, msg.text()))
  page.on("pageerror", (err) => console.log("[pageerror]", err.message))
  page.on("framenavigated", (f) => {
    if (f === page.mainFrame()) console.log("[nav]", f.url())
  })

  // 1. Tally /login page
  console.log("→ goto /login")
  await page.goto("/login", { waitUntil: "domcontentloaded" })
  await page.waitForLoadState("networkidle", { timeout: 15_000 }).catch(() => {})

  // 2. Click "使用 Lurus 账户登录" — form server action triggers OIDC
  const signInBtn = page.getByRole("button", { name: /使用.*登录|sign in|continue/i }).first()
  await signInBtn.waitFor({ state: "visible", timeout: 10_000 })
  console.log("→ click sign in")
  await signInBtn.click()

  // 3. Wait for Zitadel
  await page.waitForURL((url) => url.host === "test-auth.lurus.cn", { timeout: 30_000 })
  console.log("→ on Zitadel:", page.url())
  await page.waitForLoadState("domcontentloaded")

  // 4. Username
  const userInput = page.locator('input[name="loginName"], input[type="email"]').first()
  await userInput.waitFor({ state: "visible", timeout: 15_000 })
  await userInput.fill(ZITADEL_USER)
  console.log("→ filled username")
  await page.keyboard.press("Enter")
  await page.waitForLoadState("networkidle", { timeout: 15_000 }).catch(() => {})

  // 5. Password
  const pwdInput = page.locator('input[name="password"], input[type="password"]').first()
  await pwdInput.waitFor({ state: "visible", timeout: 15_000 })
  await pwdInput.fill(ZITADEL_PASS)
  console.log("→ filled password")
  await page.keyboard.press("Enter")

  // 6. Wait for return — handle MFA / consent loops
  console.log("→ wait for tally-stage")
  for (let i = 0; i < 20; i++) {
    if (page.url().includes("tally-stage.lurus.cn")) break
    const skip = page
      .getByRole("button", { name: /skip|continue|allow|授权|继续|允许|跳过/i })
      .first()
    if (await skip.isVisible({ timeout: 1_000 }).catch(() => false)) {
      const txt = await skip.textContent().catch(() => "")
      console.log("→ click", txt)
      await skip.click().catch(() => {})
    }
    await page.waitForTimeout(1_000)
  }
  await page.waitForURL((url) => url.host === "tally-stage.lurus.cn", { timeout: 30_000 })
  console.log("→ back on tally:", page.url())

  // 7. If on /setup, choose retail
  if (page.url().includes("/setup")) {
    console.log("→ on /setup, choosing retail")
    await page.getByRole("button", { name: "选这个" }).first().click()
    await page.waitForURL(/\/(dashboard|products)/, { timeout: 30_000 })
  }

  await page.context().storageState({ path: AUTH_FILE })
  console.log("→ auth saved:", AUTH_FILE)
  await expect(page).toHaveURL(/tally-stage\.lurus\.cn/)
})
