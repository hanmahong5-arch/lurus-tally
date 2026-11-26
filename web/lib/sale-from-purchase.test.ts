import { describe, expect, it } from "vitest"
import { linesFromPurchase, qtyText } from "./sale-from-purchase"

describe("linesFromPurchase", () => {
  const head = { bill_no: "PO-1", partner_name: "龙口老李", warehouse_id: "wh" }

  it("keeps the unsold quantity and leaves the selling price blank", () => {
    const got = linesFromPurchase(
      {
        head,
        items: [
          { id: "l1", product_id: "p1", unit_name: "卷", qty: "10.0000", batch_remaining: "1.0000" },
          { id: "l2", product_id: "p2", qty: "2", batch_remaining: "0" },
        ],
      },
      { p1: "卫生纸" }
    )
    expect(got.items).toEqual([
      {
        product_id: "p1",
        product_name: "卫生纸",
        unit_id: undefined,
        unit_name: "卷",
        line_no: 1,
        qty: "1",
        unit_price: "",
        source_item_id: "l1",
      },
    ])
    expect(got.warehouseId).toBe("wh")
    expect(got.note).toContain("龙口老李")
    expect(got.note).toContain("不会带进货价")
  })

  it("copies the original quantity when the receipt has no batch", () => {
    const got = linesFromPurchase(
      { head, items: [{ id: "l1", product_id: "p1", qty: "2.5000" }] },
      {}
    )
    expect(got.items[0].qty).toBe("2.5")
    expect(got.note).toContain("开始记批次之前")
  })

  it("trims trailing zeros", () => {
    expect(qtyText("10.0000")).toBe("10")
    expect(qtyText("1.50")).toBe("1.5")
  })
})
