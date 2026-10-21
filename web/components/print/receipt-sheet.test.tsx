import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { ReceiptSheet } from "./receipt-sheet"
import { payTypeText } from "./print-format"

const shop = { shop_name: "老王五金", phone: "", address: "", receipt_footer: "" }
const base = {
  shop,
  billNo: "SO-1",
  dateText: "2026-10-10 10:00",
  lines: [{ name: "螺丝刀", unit: "把", qty: "1", price: "12.30", amount: "12.30" }],
  totalAmount: "12.30",
  wide: true,
}

describe("ReceiptSheet payment block", () => {
  it("prints pay method, tendered cash and change", () => {
    render(<ReceiptSheet {...base} payMethod="现金" paidAmount="12.30" receivedAmount="100.00" changeAmount="87.70" />)
    expect(screen.getByText("付款方式")).toBeInTheDocument()
    expect(screen.getByText("现金")).toBeInTheDocument()
    expect(screen.getByText("实收")).toBeInTheDocument()
    expect(screen.getByText("¥100.00")).toBeInTheDocument()
    expect(screen.getByText("找零")).toBeInTheDocument()
    expect(screen.getByText("¥87.70")).toBeInTheDocument()
  })

  it("falls back to the booked amount when no tender is known", () => {
    render(<ReceiptSheet {...base} payMethod="微信" paidAmount="12.30" />)
    expect(screen.getByText("实收")).toBeInTheDocument()
    expect(screen.getAllByText("¥12.30")).toHaveLength(2)
    expect(screen.queryByText("找零")).toBeNull()
  })
})

describe("payTypeText", () => {
  it("joins distinct methods and hides raw codes", () => {
    expect(payTypeText(["cash", "cash", "wechat"])).toBe("现金、微信")
    expect(payTypeText(["weird"])).toBe("其他方式")
    expect(payTypeText([])).toBe("")
  })
})

describe("ReceiptSheet 离线单", () => {
  const entry = {
    client_ref: "abcd1234-7",
    idempotency_key: "k",
    body: {
      items: [{ product_id: "p1", qty: "2", unit_name: "把", unit_price: "12.30" }],
      payment_method: "cash" as const,
      paid_amount: "50.00",
    },
    status: "pending" as const,
    created_at: 1,
    occurred_at: new Date(2026, 9, 10, 10, 5).getTime(),
    attempts: 0,
    total_amount: "24.60",
    item_names: { p1: "螺丝刀" },
  }

  it("单号印离线号并标明待上传，金额与找零按本地数据算", () => {
    render(<ReceiptSheet kind="offline" entry={entry} shop={shop} wide />)
    expect(screen.getByText("单号：OFF-abcd-7")).toBeInTheDocument()
    expect(screen.getByText("离线单，待上传")).toBeInTheDocument()
    expect(screen.getByText("螺丝刀")).toBeInTheDocument()
    expect(screen.getByText("时间：2026-10-10 10:05")).toBeInTheDocument()
    expect(screen.getByText("找零")).toBeInTheDocument()
    expect(screen.getByText("¥25.40")).toBeInTheDocument()
  })
})
