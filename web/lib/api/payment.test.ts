import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { listPayments, recordPayment } from "./payment"

const payment = { id: "p1", bill_id: "b1", pay_type: "cash", amount: "10", pay_date: "2026-01-02T00:00:00Z" }

describe("payment api", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
  })
  afterEach(() => vi.restoreAllMocks())

  it("listPayments reads the server's items list", async () => {
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({ items: [payment] }) })
    expect(await listPayments("b1")).toEqual([payment])
  })

  it("recordPayment returns the saved payment", async () => {
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => payment })
    const saved = await recordPayment({ bill_id: "b1", amount: "10", payment_method: "cash" })
    expect(saved.id).toBe("p1")
    expect(saved.amount).toBe("10")
  })
})
