/**
 * Unit tests for the Tally billing API wrapper.
 * Backend is mocked via global.fetch — no Tally Go process required.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  BillingError,
  getBillingOverview,
  subscribe,
  type BillingOverview,
  type SubscribeResponse,
} from "./billing"

function mockFetch(ok: boolean, body: unknown, status?: number) {
  global.fetch = vi.fn().mockResolvedValueOnce({
    ok,
    status: ok ? (status ?? 200) : (status ?? 400),
    json: async () => body,
  })
}

describe("getBillingOverview", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("decodes the overview body", async () => {
    const overview: BillingOverview = {
      account: { id: 7, username: "u", email: "u@x", vip_tier: "Standard" },
      wallet: { available: 50, frozen: 0, total: 50 },
      subscription: { plan_code: "free", status: "active" },
    }
    mockFetch(true, overview)

    const got = await getBillingOverview()
    expect(got.account.email).toBe("u@x")
    expect(got.wallet?.available).toBe(50)
    expect(got.subscription?.plan_code).toBe("free")
  })

  it("throws BillingError on 401 with code unauthorized", async () => {
    mockFetch(false, { error: "unauthorized", detail: "sign-in required" }, 401)
    await expect(getBillingOverview()).rejects.toMatchObject({
      name: "BillingError",
      code: "unauthorized",
      httpStatus: 401,
    })
  })
})

describe("subscribe", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns subscription on wallet activation", async () => {
    const body: SubscribeResponse = {
      subscription: { plan_code: "pro", status: "active" },
    }
    mockFetch(true, body)

    const got = await subscribe({
      plan_code: "pro",
      billing_cycle: "monthly",
      payment_method: "wallet",
    })
    expect(got.subscription?.plan_code).toBe("pro")
    expect(got.pay_url).toBeUndefined()
  })

  it("returns pay_url for alipay", async () => {
    mockFetch(true, { order_no: "ORD42", pay_url: "https://alipay/qr/ORD42" })
    const got = await subscribe({
      plan_code: "pro",
      billing_cycle: "monthly",
      payment_method: "alipay",
    })
    expect(got.pay_url).toBe("https://alipay/qr/ORD42")
    expect(got.order_no).toBe("ORD42")
  })

  it("maps 402 to BillingError(insufficient_balance)", async () => {
    mockFetch(false, { error: "insufficient_balance", detail: "broke" }, 402)
    try {
      await subscribe({
        plan_code: "pro",
        billing_cycle: "monthly",
        payment_method: "wallet",
      })
      throw new Error("expected throw")
    } catch (err) {
      expect(err).toBeInstanceOf(BillingError)
      const be = err as BillingError
      expect(be.code).toBe("insufficient_balance")
      expect(be.httpStatus).toBe(402)
    }
  })

  it("maps 502 to BillingError(platform_unavailable)", async () => {
    mockFetch(
      false,
      { error: "platform_unavailable", detail: "boom" },
      502,
    )
    try {
      await subscribe({
        plan_code: "pro",
        billing_cycle: "monthly",
        payment_method: "wallet",
      })
      throw new Error("expected throw")
    } catch (err) {
      expect(err).toBeInstanceOf(BillingError)
      expect((err as BillingError).code).toBe("platform_unavailable")
    }
  })
})
