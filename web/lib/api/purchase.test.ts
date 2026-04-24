/**
 * Unit tests for purchase API wrapper (type-level and response-parsing).
 * These tests mock fetch so no backend is required.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import {
  createPurchaseBill,
  approvePurchaseBill,
  listPurchaseBills,
  getPurchaseBill,
  type BillHead,
  type BillDetail,
} from "./purchase"

// Helper: create a minimal BillHead fixture.
function makeBillHead(overrides: Partial<BillHead> = {}): BillHead {
  return {
    id: "00000000-0000-0000-0000-000000000001",
    tenant_id: "00000000-0000-0000-0000-000000000002",
    bill_no: "PO-20260423-0001",
    bill_type: "入库",
    sub_type: "采购",
    status: 0,
    creator_id: "00000000-0000-0000-0000-000000000003",
    bill_date: "2026-04-23T00:00:00Z",
    subtotal: "225.0000",
    shipping_fee: "10.0000",
    tax_amount: "5.0000",
    total_amount: "240.0000",
    created_at: "2026-04-23T00:00:00Z",
    updated_at: "2026-04-23T00:00:00Z",
    ...overrides,
  }
}

describe("createPurchaseBill", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns bill_id and bill_no on 201", async () => {
    const mockResp = { bill_id: "uuid-1", bill_no: "PO-20260423-0001" }
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => mockResp,
    })

    const result = await createPurchaseBill({
      items: [{ product_id: "prod-1", qty: "10", unit_price: "8.00", line_no: 1 }],
    })

    expect(result.bill_id).toBe("uuid-1")
    expect(result.bill_no).toBe("PO-20260423-0001")
  })

  it("throws on non-ok response", async () => {
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: async () => ({ message: "validation_error", error: "items required" }),
    })

    await expect(
      createPurchaseBill({ items: [] })
    ).rejects.toThrow("validation_error")
  })
})

describe("approvePurchaseBill", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns BillHead on 200", async () => {
    const head = makeBillHead({ status: 2 })
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => head,
    })

    const result = await approvePurchaseBill("some-bill-id")
    expect(result.status).toBe(2)
  })
})

describe("listPurchaseBills", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    // Provide window.location for URL construction
    Object.defineProperty(global, "window", {
      value: { location: { origin: "http://localhost:3000" } },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns items and total", async () => {
    const items = [makeBillHead()]
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ items, total: 1 }),
    })

    const result = await listPurchaseBills({ page: 1, size: 20 })
    expect(result.total).toBe(1)
    expect(result.items).toHaveLength(1)
    expect(result.items[0].bill_no).toBe("PO-20260423-0001")
  })
})

describe("getPurchaseBill", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns BillDetail with head and items", async () => {
    const detail: BillDetail = {
      head: makeBillHead(),
      items: [],
    }
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => detail,
    })

    const result = await getPurchaseBill("some-id")
    expect(result.head.bill_no).toBe("PO-20260423-0001")
    expect(Array.isArray(result.items)).toBe(true)
  })
})
