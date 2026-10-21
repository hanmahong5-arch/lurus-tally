/**
 * Wire shape of the two return calls: the path, the body and the Idempotency-Key the server now
 * demands (a replayed return would put stock back, or take it out, twice).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { returnPurchaseBill } from "./purchase"
import { returnSaleBill } from "./sale"

function lastCall(): { url: string; init: RequestInit } {
  const [url, init] = (global.fetch as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
  return { url: String(url), init: init as RequestInit }
}

function header(init: RequestInit, name: string): string | undefined {
  const h = init.headers as Record<string, string> | Headers
  return h instanceof Headers ? (h.get(name) ?? undefined) : h[name]
}

describe("return calls", () => {
  beforeEach(() => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 201, json: async () => ({ bill_id: "r-1", bill_no: "R-1", return_amount: "1" }) })
  })
  afterEach(() => vi.restoreAllMocks())

  it("returnPurchaseBill posts the lines to the purchase and sends the caller's key", async () => {
    await returnPurchaseBill("po-1", { items: [{ bill_item_id: "l-1", qty: "3" }] }, undefined, "key-123")
    const { url, init } = lastCall()
    expect(url).toContain("/purchase-bills/po-1/return")
    expect(init.method).toBe("POST")
    expect(JSON.parse(init.body as string)).toEqual({ items: [{ bill_item_id: "l-1", qty: "3" }] })
    expect(header(init, "Idempotency-Key")).toBe("key-123")
  })

  it("returnSaleBill carries the refund fields and a key even when the caller gives none", async () => {
    await returnSaleBill("so-1", {
      items: [{ bill_item_id: "l-1", qty: "2" }],
      refund_amount: "40.00",
      refund_method: "wechat",
    })
    const { url, init } = lastCall()
    expect(url).toContain("/sale-bills/so-1/return")
    expect(JSON.parse(init.body as string)).toMatchObject({ refund_amount: "40.00", refund_method: "wechat" })
    expect(header(init, "Idempotency-Key")).toBeTruthy()
  })
})
