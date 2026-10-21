import { describe, it, expect } from "vitest"
import { PRODUCT_IMPORT_TEMPLATE, productImportTemplate } from "./products"

describe("productImportTemplate", () => {
  it("gives a nursery its own example row, same header", () => {
    const t = productImportTemplate("horticulture")
    expect(t.split("\n")[0]).toBe(PRODUCT_IMPORT_TEMPLATE.split("\n")[0])
    expect(t).toContain("香樟")
    expect(t).toContain("株")
  })
  it("gives a cross-border shop a cross-border example row", () => {
    const t = productImportTemplate("cross_border")
    expect(t.split("\n")[0]).toBe(PRODUCT_IMPORT_TEMPLATE.split("\n")[0])
    expect(t).toContain("蓝牙耳机")
    expect(t).not.toContain("螺丝")
  })
  it("keeps the default row for other trades", () => {
    expect(productImportTemplate("retail")).toBe(PRODUCT_IMPORT_TEMPLATE)
    expect(productImportTemplate(null)).toBe(PRODUCT_IMPORT_TEMPLATE)
  })
})
