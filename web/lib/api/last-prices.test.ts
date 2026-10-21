import { describe, it, expect, vi, beforeEach } from "vitest"

vi.mock("./client", () => ({ apiFetch: vi.fn() }))

import { apiFetch } from "./client"
import { getLastPrices, formatLastPriceHint } from "./last-prices"

describe("last-prices", () => {
  beforeEach(() => vi.mocked(apiFetch).mockReset())

  it("formatLastPriceHint renders price and month/day", () => {
    expect(formatLastPriceHint("9.80", "2026-09-28")).toBe("上次 ¥9.80 · 9/28")
    expect(formatLastPriceHint("9.80", "2026-10-05")).toBe("上次 ¥9.80 · 10/5")
    expect(formatLastPriceHint("9.80", "")).toBe("上次 ¥9.80")
  })

  it("formatLastPriceHint shows the unit, base price and default source", () => {
    expect(formatLastPriceHint("42.00", "2026-10-02", { unitName: "箱" })).toBe("上次 ¥42.00/箱 · 10/2")
    expect(formatLastPriceHint("42.00", "2026-10-02", { unitName: "箱", basePrice: "3.50" })).toBe(
      "上次 ¥42.00/箱 · 10/2（折合每基本单位 ¥3.50）"
    )
    expect(formatLastPriceHint("12.50", "", { source: "default" })).toBe("默认价 ¥12.50")
  })

  it("getLastPrices sends partner, products and type", async () => {
    vi.mocked(apiFetch).mockResolvedValue({ items: [] })
    await getLastPrices({ partnerId: "c1", productIds: ["a", "b"], type: "sale" })
    const url = vi.mocked(apiFetch).mock.calls[0][0] as string
    expect(url).toContain("/last-prices?")
    expect(url).toContain("partner_id=c1")
    expect(url).toContain("product_ids=a%2Cb")
    expect(url).toContain("type=sale")
  })
})
