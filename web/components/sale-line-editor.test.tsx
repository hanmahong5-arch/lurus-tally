import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { useState } from "react"

vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t1" }))
vi.mock("@/lib/api/last-prices", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/last-prices")>()),
  getLastPrices: vi.fn(),
}))
vi.mock("@/components/entity-pickers", () => ({
  ProductPicker: ({ onSelect }: { onSelect: (o: { id: string; name: string }) => void }) => (
    <button type="button" onClick={() => onSelect({ id: "p1", name: "红枫" })}>
      pick-product
    </button>
  ),
}))

import { getLastPrices } from "@/lib/api/last-prices"
import { SaleLineEditor, type SaleLineItem } from "./sale-line-editor"

function Harness({ partnerId }: { partnerId?: string }) {
  const [items, setItems] = useState<SaleLineItem[]>([
    { product_id: "", qty: "1", unit_price: "0", line_no: 1, _subtotal: "0" },
  ])
  return <SaleLineEditor items={items} onChange={setItems} partnerId={partnerId} />
}

describe("SaleLineEditor last price", () => {
  beforeEach(() => vi.mocked(getLastPrices).mockReset())

  it("fills the partner's last price and shows the hint", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [{ product_id: "p1", unit_price: "9.80", unit_name: "株", base_unit_price: "9.80", factor: "1", source: "last", bill_date: "2026-09-28" }],
    })
    render(<Harness partnerId="c1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("9.80")).toBeTruthy())
    expect(screen.getByText("上次 ¥9.80/株 · 9/28")).toBeTruthy()
    expect(vi.mocked(getLastPrices).mock.calls[0][0]).toMatchObject({ partnerId: "c1", productIds: ["p1"], type: "sale" })
  })

  it("a price quoted per carton is converted to the base unit this editor bills in", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [
        { product_id: "p1", unit_price: "42.00", unit_id: "u-box", unit_name: "箱", base_unit_price: "3.50", factor: "12", source: "last", bill_date: "2026-10-02" },
      ],
    })
    render(<Harness partnerId="c1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("3.50")).toBeTruthy())
    expect(screen.queryByDisplayValue("42.00")).toBeNull()
    expect(screen.getByText("上次 ¥42.00/箱 · 10/2（折合每基本单位 ¥3.50）")).toBeTruthy()
  })

  it("a customer without history gets the default price, labelled as such", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [
        { product_id: "p1", unit_price: "12.50", unit_name: "件", base_unit_price: "12.50", factor: "1", source: "default", bill_date: "" },
      ],
    })
    render(<Harness partnerId="c2" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("12.50")).toBeTruthy())
    expect(screen.getByText("默认价 ¥12.50/件")).toBeTruthy()
    expect(screen.queryByText(/上次/)).toBeNull()
  })

  it("keeps the price when the partner has no history", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({ items: [] })
    render(<Harness partnerId="c2" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(getLastPrices).toHaveBeenCalled())
    expect(screen.queryByText(/上次/)).toBeNull()
    expect(screen.getByDisplayValue("0")).toBeTruthy()
  })

  it("a walk-in sale (no customer) still gets the default price", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [{ product_id: "p1", unit_price: "12.50", unit_name: "件", base_unit_price: "12.50", factor: "1", source: "default", bill_date: "" }],
    })
    render(<Harness />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("12.50")).toBeTruthy())
    expect(vi.mocked(getLastPrices).mock.calls[0][0].partnerId).toBeUndefined()
    expect(screen.getByText("默认价 ¥12.50/件")).toBeTruthy()
  })

  it("re-picking the product on a copied line drops the old product's unit", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({ items: [] })
    const onChange = vi.fn()
    render(
      <SaleLineEditor
        onChange={onChange}
        items={[{ product_id: "p0", product_name: "旧商品", qty: "2", unit_price: "5", line_no: 1, unit_id: "u-old", unit_name: "盆" }]}
      />
    )
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(onChange).toHaveBeenCalled())
    const row = onChange.mock.calls[0][0][0] as SaleLineItem
    expect(row.product_id).toBe("p1")
    expect(row.unit_id).toBeUndefined()
    expect(row.unit_name).toBeUndefined()
  })

  it("a converted per-base-unit price is rounded to the cent on the sale line", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [
        { product_id: "p1", unit_price: "40.00", unit_id: "u-box", unit_name: "箱", base_unit_price: "3.333333", factor: "12", source: "last", bill_date: "2026-10-02" },
      ],
    })
    render(<Harness partnerId="c1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("3.33")).toBeTruthy())
    expect(screen.queryByDisplayValue("3.333333")).toBeNull()
    // the line total follows the rounded price
    expect(screen.getByText("3.33")).toBeTruthy()
  })

  it("a price the clerk typed is never rounded away", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({ items: [] })
    const onChange = vi.fn()
    render(
      <SaleLineEditor
        onChange={onChange}
        items={[{ product_id: "p0", product_name: "红枫", qty: "1", unit_price: "1.005", line_no: 1, _price_touched: true }]}
      />
    )
    expect(screen.getByDisplayValue("1.005")).toBeTruthy()
  })

  it("read-only line with unknown product never shows an id", () => {
    render(
      <SaleLineEditor
        readOnly
        onChange={() => {}}
        items={[{ product_id: "0b1c2d3e-0000-0000-0000-000000000000", qty: "1", unit_price: "5", line_no: 1 }]}
      />
    )
    expect(screen.getByText("未知商品")).toBeTruthy()
    expect(screen.queryByText(/0b1c2d3e/)).toBeNull()
  })
})
