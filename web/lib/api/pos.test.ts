import { describe, it, expect, beforeEach, afterEach, vi } from "vitest"
import {
  quickCheckout,
  listTodaySaleBills,
  type QuickCheckoutRequest,
} from "./pos"

// Simple fetch mock — no MSW dependency needed for unit tests of the wrapper.
const mockFetch = vi.fn()

beforeEach(() => {
  mockFetch.mockReset()
  vi.stubGlobal("fetch", mockFetch)
  // window.location.origin is needed by listTodaySaleBills
  Object.defineProperty(window, "location", {
    value: { origin: "http://localhost:3000" },
    writable: true,
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

function makeOkResponse(body: unknown) {
  return Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve(body),
  })
}

function makeErrorResponse(status: number, body: unknown) {
  return Promise.resolve({
    ok: false,
    status,
    json: () => Promise.resolve(body),
  })
}

const sampleRequest: QuickCheckoutRequest = {
  items: [
    {
      product_id: "prod-1",
      warehouse_id: "wh-1",
      qty: "2",
      unit_price: "9.99",
    },
  ],
  payment_method: "cash",
  paid_amount: "19.98",
}

describe("quickCheckout", () => {
  it("TestPosApi_QuickCheckout_ReturnsResult: successful 201 returns QuickCheckoutResult", async () => {
    const mockResult = {
      bill_id: "bill-uuid",
      bill_no: "SO-2024-0001",
      total_amount: "19.98",
      receivable_amount: "19.98",
    }
    mockFetch.mockReturnValue(
      Promise.resolve({
        ok: true,
        status: 201,
        json: () => Promise.resolve(mockResult),
      })
    )

    const result = await quickCheckout(sampleRequest)
    expect(result.bill_id).toBe("bill-uuid")
    expect(result.bill_no).toBe("SO-2024-0001")
    expect(result.total_amount).toBe("19.98")
    expect(result.receivable_amount).toBe("19.98")
  })

  it("TestPosApi_QuickCheckout_InsufficientStock_Throws: 422 response throws with insufficient_stock", async () => {
    mockFetch.mockReturnValue(
      makeErrorResponse(422, {
        error: "insufficient_stock",
        product_id: "prod-1",
        available: 1,
        requested: 2,
      })
    )

    await expect(quickCheckout(sampleRequest)).rejects.toThrow("insufficient_stock")
  })

  it("TestPosApi_QuickCheckout_ForwardsTenantId: X-Tenant-ID header is set when tenantId provided", async () => {
    mockFetch.mockReturnValue(
      Promise.resolve({
        ok: true,
        status: 201,
        json: () => Promise.resolve({ bill_id: "x", bill_no: "y", total_amount: "0", receivable_amount: "0" }),
      })
    )

    await quickCheckout(sampleRequest, "tenant-abc")
    const [, init] = mockFetch.mock.calls[0]
    const headers = init?.headers as Record<string, string>
    expect(headers["X-Tenant-ID"]).toBe("tenant-abc")
  })
})

describe("listTodaySaleBills", () => {
  it("TestPosApi_ListTodaySales_ReturnsArray: returns array from API response", async () => {
    const mockBills = [
      {
        id: "b1",
        bill_no: "SO-001",
        total_amount: "99.00",
        paid_amount: "99.00",
        payment_method: "cash",
        created_at: "2024-01-01T10:00:00Z",
      },
    ]
    mockFetch.mockReturnValue(makeOkResponse({ items: mockBills, total: 1 }))

    const result = await listTodaySaleBills()
    expect(Array.isArray(result)).toBe(true)
    expect(result).toHaveLength(1)
    expect(result[0].bill_no).toBe("SO-001")
  })

  it("TestPosApi_ListTodaySales_Empty_ReturnsEmptyArray", async () => {
    mockFetch.mockReturnValue(makeOkResponse({ items: [], total: 0 }))

    const result = await listTodaySaleBills()
    expect(result).toHaveLength(0)
  })

  it("TestPosApi_ListTodaySales_IncludesDateParams: URL contains date_from and date_to", async () => {
    mockFetch.mockReturnValue(makeOkResponse({ items: [], total: 0 }))

    await listTodaySaleBills()
    const [url] = mockFetch.mock.calls[0]
    expect(url).toContain("date_from=")
    expect(url).toContain("date_to=")
  })
})
