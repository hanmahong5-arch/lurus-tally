import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { DocumentSheet } from "./document-sheet"

const shop = { shop_name: "", phone: "", address: "", receipt_footer: "" }
const base = {
  shop,
  title: "送货单",
  billNo: "SO-1",
  dateText: "2026-10-10",
  meta: [["客户", "老张"]] as [string, string][],
  totalAmount: "105.00",
  signLabels: ["送货人"],
}

describe("DocumentSheet", () => {
  it("adds a spec column only when a line has a spec", () => {
    const { rerender } = render(<DocumentSheet {...base} lines={[{ name: "雪松", qty: "3", price: "35", amount: "105" }]} />)
    expect(screen.queryByText("规格", { selector: "th" })).toBeNull()
    rerender(<DocumentSheet {...base} lines={[{ name: "雪松", spec: "胸径3cm 冠幅1.2m", qty: "3", price: "35", amount: "105" }]} />)
    expect(screen.getByText("规格", { selector: "th" })).toBeInTheDocument()
    expect(screen.getByText("胸径3cm 冠幅1.2m")).toBeInTheDocument()
  })

  it("widens the quantity and amount columns", () => {
    render(<DocumentSheet {...base} lines={[{ name: "雪松", qty: "3", price: "35", amount: "105" }]} />)
    expect(screen.getByText("数量", { selector: "th" }).className).toContain("w-20")
    expect(screen.getByText("金额", { selector: "th" }).className).toContain("w-28")
  })

  it("gives the spec column more room and the price column less on A5", () => {
    render(<DocumentSheet {...base} paper="a5" lines={[{ name: "雪松", spec: "胸径3cm 冠幅1.2m", qty: "3", price: "35", amount: "105" }]} />)
    expect(screen.getByText("规格", { selector: "th" }).className).toContain("w-24")
    expect(screen.getByText("单价", { selector: "th" }).className).toContain("w-16")
  })

  it("prints wide rows only when they have a value", () => {
    render(<DocumentSheet {...base} metaWide={[["收货地址", "城北基地"], ["备用", ""]]} lines={[]} />)
    expect(screen.getByText("收货地址：城北基地")).toBeInTheDocument()
    expect(screen.queryByText(/备用/)).toBeNull()
  })
})
