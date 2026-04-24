/**
 * Unit tests for currency API wrapper (type-level and response-parsing).
 * These tests mock fetch so no backend is required.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import {
  getCurrencies,
  getRateOn,
  createRate,
  getRateHistory,
  type Currency,
  type RateResult,
  type ExchangeRate,
} from "./currency"

// Provide window.location for URL construction used in getRateOn / getRateHistory.
Object.defineProperty(global, "window", {
  value: { location: { origin: "http://localhost:3000" } },
  writable: true,
})

function mockFetch(ok: boolean, body: unknown) {
  global.fetch = vi.fn().mockResolvedValueOnce({
    ok,
    status: ok ? 200 : 400,
    json: async () => body,
  })
}

describe("getCurrencies", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns array of currencies", async () => {
    const currencies: Currency[] = [
      { code: "CNY", name: "人民币", symbol: "¥", enabled: true },
      { code: "USD", name: "美元", symbol: "$", enabled: true },
    ]
    mockFetch(true, { currencies })

    const result = await getCurrencies()
    expect(result).toHaveLength(2)
    expect(result[0].code).toBe("CNY")
  })

  it("throws on non-ok response", async () => {
    mockFetch(false, { message: "internal error" })
    await expect(getCurrencies()).rejects.toThrow("internal error")
  })
})

describe("getRateOn", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns rate result on 200", async () => {
    const rateResult: RateResult = { rate: "7.25", source: "manual" }
    mockFetch(true, rateResult)

    const result = await getRateOn("USD", "CNY", "2026-04-23")
    expect(result.rate).toBe("7.25")
    expect(result.source).toBe("manual")
    expect(result.warning).toBeUndefined()
  })

  it("returns default fallback when no data", async () => {
    const fallback: RateResult = { rate: "1", source: "default", warning: "no_rate_found" }
    mockFetch(true, fallback)

    const result = await getRateOn("USD", "CNY")
    expect(result.warning).toBe("no_rate_found")
    expect(result.source).toBe("default")
    expect(result.rate).toBe("1")
  })
})

describe("createRate", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns created ExchangeRate on 201", async () => {
    const created: ExchangeRate = {
      id: "uuid-1",
      tenant_id: "tenant-1",
      from_currency: "USD",
      to_currency: "CNY",
      rate: "7.25",
      source: "manual",
      effective_at: "2026-04-23T00:00:00Z",
      created_at: "2026-04-23T00:00:00Z",
    }
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 201,
      json: async () => created,
    })

    const result = await createRate(
      { from_currency: "USD", to_currency: "CNY", rate: "7.25" },
      "tenant-1"
    )
    expect(result.id).toBe("uuid-1")
    expect(result.source).toBe("manual")
  })

  it("throws on non-ok response", async () => {
    mockFetch(false, { message: "invalid rate" })
    await expect(
      createRate({ from_currency: "USD", to_currency: "CNY", rate: "0" })
    ).rejects.toThrow("invalid rate")
  })
})

describe("getRateHistory", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns rates array", async () => {
    const rates: ExchangeRate[] = [
      {
        id: "uuid-1",
        tenant_id: "t1",
        from_currency: "USD",
        to_currency: "CNY",
        rate: "7.20",
        source: "manual",
        effective_at: "2026-04-20T00:00:00Z",
        created_at: "2026-04-20T00:00:00Z",
      },
    ]
    mockFetch(true, { rates })

    const result = await getRateHistory("USD", "CNY", 30)
    expect(result).toHaveLength(1)
    expect(result[0].rate).toBe("7.20")
  })

  it("returns empty array when rates is null", async () => {
    mockFetch(true, { rates: null })
    const result = await getRateHistory("USD", "CNY")
    expect(result).toEqual([])
  })
})
