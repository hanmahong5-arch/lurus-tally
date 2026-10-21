import { describe, it, expect, vi } from "vitest"
import { ApiError } from "@/lib/api/errors"

vi.mock("@/lib/api/products", () => ({ getProduct: vi.fn() }))
vi.mock("@/lib/api/units", () => ({ listUnits: vi.fn() }))

import { getProduct } from "@/lib/api/products"
import { listUnits } from "@/lib/api/units"
import { describeStockShortage, parseStockShortages } from "./stock-shortage"

const shortErr = (details: unknown[]) =>
  new ApiError(422, "insufficient_stock", "HTTP 422", { error: "insufficient_stock", message: "库存不足", details })

describe("stock shortage", () => {
  it("names the product and the gap", async () => {
    vi.mocked(getProduct).mockResolvedValue({ id: "p1", name: "雪松" } as never)
    const msg = await describeStockShortage(shortErr([{ product_id: "p1", available_qty: "2", requested_qty: "5" }]))
    expect(msg).toBe("「雪松」库存不足：可用 2，本单需要 5")
  })

  it("names the base unit after the quantities", async () => {
    vi.mocked(getProduct).mockResolvedValue({ id: "p1", name: "雪松", default_unit_id: "u1" } as never)
    vi.mocked(listUnits).mockResolvedValue({ items: [{ id: "u1", name: "株" }] } as never)
    const msg = await describeStockShortage(shortErr([{ product_id: "p1", available_qty: "0", requested_qty: "48" }]))
    expect(msg).toBe("「雪松」库存不足：可用 0株，本单需要 48株")
  })

  it("prefers known names and falls back when the lookup fails", async () => {
    vi.mocked(getProduct).mockRejectedValue(new Error("x"))
    const msg = await describeStockShortage(
      shortErr([
        { product_id: "p1", available_qty: "0", requested_qty: "1" },
        { product_id: "p2", available_qty: "1", requested_qty: "3" },
      ]),
      undefined,
      { p1: "香樟" },
    )
    expect(msg).toBe("「香樟」库存不足：可用 0，本单需要 1；「该商品」库存不足：可用 1，本单需要 3")
  })

  it("ignores other errors", async () => {
    expect(parseStockShortages(new ApiError(400, "x", "y", { error: "other" }))).toBeNull()
    expect(await describeStockShortage(new Error("boom"))).toBeNull()
  })
})
