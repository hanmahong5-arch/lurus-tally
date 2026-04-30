import { test, expect } from "@playwright/test"

// Verifies P0 wiring: re-calling POST /api/v1/tenant/profile for an already
// onboarded user is idempotent (returns 200) AND triggers the platform
// account heal path so /billing/overview succeeds afterwards. Empty-email
// Zitadel users (admin / phone-OTP) get a synthesized <sub>@zitadel.local
// placeholder so platform always owns a canonical account row.
test("upsert heals returning user → /billing/overview 200", async ({ request }) => {
  const sessionRes = await request.get("/api/auth/session")
  const session = await sessionRes.json()
  expect(session.accessToken, "id_token in session").toBeTruthy()
  const auth = { Authorization: `Bearer ${session.accessToken}` }

  const meRes = await request.get("/api/v1/me", { headers: auth })
  expect(meRes.ok(), "GET /me").toBe(true)
  const me = await meRes.json()
  expect(me.is_first_time, "user must already be onboarded for heal path").toBe(false)

  // Re-call with the same profile_type — 200 no-op, but ALSO calls
  // platformclient.UpsertAccount under the hood (idempotent on platform).
  const upsertRes = await request.post("/api/v1/tenant/profile", {
    headers: { ...auth, "Content-Type": "application/json" },
    data: { profile_type: me.profile_type },
  })
  expect(upsertRes.ok(), "POST /tenant/profile (idempotent)").toBe(true)

  // /billing/overview should now return 200 — platform has an account row.
  const overviewRes = await request.get("/api/v1/billing/overview", { headers: auth })
  expect(overviewRes.ok(), `GET /billing/overview status=${overviewRes.status()}`).toBe(true)
  const overview = await overviewRes.json()
  expect(overview.account, "overview.account").toBeTruthy()
  expect(overview.account.id, "platform account_id assigned").toBeGreaterThan(0)
})
