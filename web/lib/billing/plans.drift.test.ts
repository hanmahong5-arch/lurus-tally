/**
 * Guards against plan-catalog drift between this frontend mirror and the
 * platform-side seed migration that backend checkout actually resolves
 * against.
 *
 * Business risk under test: this is the exact same failure class as the
 * "pool slug mismatch" incident (customer's payment succeeds, but the
 * backend can't find the entitlement to grant because the identifier it was
 * seeded with doesn't match what the caller sent). Here the identifier pair
 * is (plan_code, billing_cycle): a customer picks "专业版" on this page,
 * clicks 订阅, picks a payment method, pays — and POST
 * /internal/v1/subscriptions/checkout 404s on platform's side because the
 * plan_code/billing_cycle pair this file renders doesn't match what
 * migrations/025_seed_tally_product.sql actually seeded. The failure surfaces
 * AFTER money has moved, not before.
 *
 * TALLY_PLANS itself carries a top-of-file comment saying exactly this must
 * stay in sync — this test turns that comment into something that actually
 * breaks CI/local `bun run test` on drift, instead of relying on someone
 * remembering to check both files by hand.
 *
 * The fixture below mirrors 2l-svc-platform/migrations/025_seed_tally_product.sql
 * (identity.product_plans rows for product_id='lurus-tally') as read at the
 * time this test was written. If backend seeds a NEW plan or cycle, this test
 * SHOULD fail until both this fixture and lib/billing/plans.ts are updated
 * together — that's the point.
 */
import { describe, it, expect } from "vitest"
import { TALLY_PLANS } from "./plans"

const BACKEND_SEEDED_PLANS = [
  { code: "free", cycle: "forever" },
  { code: "pro", cycle: "monthly" },
  { code: "pro_yearly", cycle: "yearly" },
  { code: "enterprise", cycle: "monthly" },
  { code: "enterprise_yearly", cycle: "yearly" },
] as const

describe("TALLY_PLANS vs platform-seeded plan catalog (025_seed_tally_product.sql)", () => {
  it("every plan_code + billing_cycle pair the frontend renders exists in the backend seed, in the same order", () => {
    const actual = TALLY_PLANS.map((p) => ({ code: p.code, cycle: p.cycle }))
    expect(actual).toEqual(BACKEND_SEEDED_PLANS)
  })

  it("no plan_code is duplicated with a different cycle by accident (would make the subscribe button ambiguous)", () => {
    const codes = TALLY_PLANS.map((p) => p.code)
    expect(new Set(codes).size).toBe(codes.length)
  })
})
