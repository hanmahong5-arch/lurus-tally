/**
 * Unit tests for sale API wrapper (type-level and response-parsing).
 * These tests mock fetch so no backend is required.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import {
  createSaleBill,
  approveSaleBill,
  listSaleBills,
  getSaleBill,
  quickCheckout,
  type SaleBillHead,
  type SaleBillDetail,
  type QuickCheckoutResult,
} from "./sale"

function makeSaleHead(overrides: Partial<SaleBillHead> = {}): SaleBillHead {
  return {
    id: "00000000-0000-0000-0000-000000000001",
    tenant_id: "00000000-0000-0000-0000-000000000002",
    bill_no: "SL-20260423-0001",
    bill_type: "出库",
    sub_type: "销售",
    status: 0,
    creator_id: "00000000-0000-0000-0000-000000000003",
    bill_date: "2026-04-23T00:00:00Z",
    subtotal: "200.0000",
    shipping_fee: "0.0000",
    tax_amount: "0.0000",
    total_amount: "200.0000",
    paid_amount: "0.0000",
    receivable_amount: "200.0000",
    created_at: "2026-04-23T00:00:00Z",
    updated_at: "2026-04-23T00:00:00Z",
    ...overrides,
  }
}

describe("createSaleBill", () => {
  beforeEach(() => { global.fetch = vi.fn() })
  afterEach(() => vi.restoreAllMocks())

  it("returns bill_id and bill_no on 201", async () => {
    const mockResp = { bill_id: "uuid-1", bill_no: "SL-20260423-0001" }
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => mockResp,
    })

    const result = await createSaleBill({
      items: [{ product_id: "prod-1", qty: "5", unit_price: "40.00", line_no: 1 }],
    })

    expect(result.bill_id).toBe("uuid-1")
    expect(result.bill_no).toBe("SL-20260423-0001")
  })

  it("throws on non-ok response", async () => {
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: async () => ({ message: "items required" }),
    })

    await expect(
      createSaleBill({ items: [] })
    ).rejects.toThrow("items required")
  })
})

describe("approveSaleBill", () => {
  beforeEach(() => { global.fetch = vi.fn() })
  afterEach(() => vi.restoreAllMocks())

  it("returns SaleBillHead with status 2 on approve", async () => {
    const head = makeSaleHead({ status: 2 })
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => head,
    })

    const result = await approveSaleBill("some-id", { paid_amount: "200", payment_method: "cash" })
    expect(result.status).toBe(2)
    expect(result.bill_no).toBe("SL-20260423-0001")
  })
})

describe("listSaleBills", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(global, "window", {
      value: { location: { origin: "http://localhost:3000" } },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("returns typed items array with receivable_amount", async () => {
    const items = [makeSaleHead()]
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ items, total: 1 }),
    })

    const result = await listSaleBills({ page: 1, size: 20 })
    expect(result.total).toBe(1)
    expect(result.items).toHaveLength(1)
    expect(result.items[0].receivable_amount).toBe("200.0000")
  })
})

describe("getSaleBill", () => {
  beforeEach(() => { global.fetch = vi.fn() })
  afterEach(() => vi.restoreAllMocks())

  it("returns SaleBillDetail with head, items and payments", async () => {
    const detail: SaleBillDetail = {
      head: makeSaleHead(),
      items: [],
      payments: [],
    }
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => detail,
    })

    const result = await getSaleBill("some-id")
    expect(result.head.bill_no).toBe("SL-20260423-0001")
    expect(Array.isArray(result.payments)).toBe(true)
  })
})

describe("quickCheckout", () => {
  beforeEach(() => { global.fetch = vi.fn() })
  afterEach(() => vi.restoreAllMocks())

  it("returns QuickCheckoutResult on success", async () => {
    const resp: QuickCheckoutResult = {
      bill_id: "uuid-2",
      bill_no: "SL-20260423-0002",
      total_amount: "500.0000",
      receivable_amount: "0.0000",
    }
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => resp,
    })

    const result = await quickCheckout({
      items: [{ product_id: "p1", qty: "5", unit_price: "100.00", line_no: 1 }],
      payment_method: "cash",
      paid_amount: "500",
    })

    expect(result.bill_id).toBe("uuid-2")
    expect(result.receivable_amount).toBe("0.0000")
  })

  it("throws on non-ok response with error message", async () => {
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      status: 422,
      json: async () => ({ message: "insufficient_stock", product_id: "p1" }),
    })

    await expect(
      quickCheckout({
        items: [{ product_id: "p1", qty: "9999", unit_price: "100.00", line_no: 1 }],
        payment_method: "cash",
        paid_amount: "999900",
      })
    ).rejects.toThrow("insufficient_stock")
  })
})
