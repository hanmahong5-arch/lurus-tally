import { describe, expect, it } from "vitest"
import { isTaxQR } from "./tax-qr"

describe("isTaxQR", () => {
  it("keeps a tax code and drops a product barcode", () => {
    expect(isTaxQR("01,32,,24317000000012345678,113.00,20260315")).toBe(true)
    expect(isTaxQR("https://inv-veri.chinatax.gov.cn")).toBe(true)
    expect(isTaxQR("6901234567890")).toBe(false)
    expect(isTaxQR("")).toBe(false)
  })
})
