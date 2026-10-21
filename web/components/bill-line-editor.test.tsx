import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { useState } from "react"

vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t1" }))
vi.mock("@/lib/api/last-prices", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/last-prices")>()),
  getLastPrices: vi.fn(),
}))
vi.mock("@/lib/api/product-units", () => ({ listProductUnits: vi.fn() }))
vi.mock("@/components/entity-pickers", () => ({
  ProductPicker: ({ onSelect }: { onSelect: (o: { id: string; name: string }) => void }) => (
    <button type="button" onClick={() => onSelect({ id: "p1", name: "红枫" })}>
      pick-product
    </button>
  ),
}))

import { getLastPrices } from "@/lib/api/last-prices"
import { listProductUnits } from "@/lib/api/product-units"
import { BillLineEditor, type BillLineItem } from "./bill-line-editor"

function Harness({ partnerId }: { partnerId?: string }) {
  const [items, setItems] = useState<BillLineItem[]>([
    { product_id: "", qty: "1", unit_price: "0", line_no: 1, _subtotal: "0" },
  ])
  return (
    <BillLineEditor
      items={items}
      onChange={setItems}
      partnerId={partnerId}
      shippingFee="0"
      taxAmount="0"
      onShippingFeeChange={() => {}}
      onTaxAmountChange={() => {}}
    />
  )
}

describe("BillLineEditor units and last price", () => {
  beforeEach(() => {
    vi.mocked(getLastPrices).mockReset()
    vi.mocked(listProductUnits).mockReset()
  })

  it("lists only the product's units and starts on the base unit", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({ items: [] })
    vi.mocked(listProductUnits).mockResolvedValue([
      { id: "u-base", code: "pcs", name: "株", conversion_factor: "1", is_base: true },
      { id: "u-box", code: "box", name: "捆", conversion_factor: "10", is_base: false },
    ])
    render(<Harness />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByRole("option", { name: "捆" })).toBeTruthy())
    expect(screen.queryByRole("option", { name: "公斤" })).toBeNull()
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-base"))
  })

  it("fills the supplier's last purchase price", async () => {
    vi.mocked(listProductUnits).mockResolvedValue([])
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [{ product_id: "p1", unit_price: "3.50", unit_name: "株", base_unit_price: "3.50", factor: "1", source: "last", bill_date: "2026-08-02" }],
    })
    render(<Harness partnerId="s1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByText("上次 ¥3.50/株 · 8/2")).toBeTruthy())
    expect(screen.getByDisplayValue("3.50")).toBeTruthy()
    expect(vi.mocked(getLastPrices).mock.calls[0][0]).toMatchObject({ type: "purchase" })
  })

  it("a hand-typed price survives a partner change", async () => {
    vi.mocked(listProductUnits).mockResolvedValue([])
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [{ product_id: "p1", unit_price: "3.50", unit_name: "株", base_unit_price: "3.50", factor: "1", source: "last", bill_date: "2026-08-02" }],
    })
    const { rerender } = render(<Harness partnerId="s1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => screen.getByDisplayValue("3.50"))
    fireEvent.change(screen.getByDisplayValue("3.50"), { target: { value: "4" } })
    rerender(<Harness partnerId="s2" />)
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.getByDisplayValue("4")).toBeTruthy()
  })

  it("a price quoted per carton fills the carton unit, not the base unit", async () => {
    vi.mocked(listProductUnits).mockResolvedValue([
      { id: "u-base", code: "pcs", name: "件", conversion_factor: "1", is_base: true },
      { id: "u-box", code: "box", name: "箱", conversion_factor: "12", is_base: false },
    ])
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [
        { product_id: "p1", unit_price: "42.00", unit_id: "u-box", unit_name: "箱", base_unit_price: "3.50", factor: "12", source: "last", bill_date: "2026-10-02" },
      ],
    })
    render(<Harness partnerId="s1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("42.00")).toBeTruthy())
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-box"))
    expect(screen.getByText("上次 ¥42.00/箱 · 10/2")).toBeTruthy()
    // 12 cartons-worth is not mistaken for 12 pieces: subtotal is 1 x 42.00.
    expect(screen.getByText("42.0000", { selector: "td" })).toBeTruthy()
  })

  it("a supplier without history gets the default purchase price", async () => {
    vi.mocked(listProductUnits).mockResolvedValue([])
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [
        { product_id: "p1", unit_price: "3.00", unit_name: "件", base_unit_price: "3.00", factor: "1", source: "default", bill_date: "" },
      ],
    })
    render(<Harness partnerId="s2" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("3.00")).toBeTruthy())
    expect(screen.getByText("默认价 ¥3.00/件")).toBeTruthy()
    expect(screen.queryByText(/上次/)).toBeNull()
  })

  it("re-picking the same product puts the base unit back", async () => {
    vi.mocked(getLastPrices).mockResolvedValue({ items: [] })
    vi.mocked(listProductUnits).mockResolvedValue([
      { id: "u-base", code: "pcs", name: "件", conversion_factor: "1", is_base: true },
      { id: "u-box", code: "box", name: "箱", conversion_factor: "12", is_base: false },
    ])
    render(<Harness />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-base"))
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "u-box" } })
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-base"))
    expect(listProductUnits).toHaveBeenCalledTimes(1)
  })

  it("a late unit-list response does not undo the price filled meanwhile", async () => {
    let resolveUnits: (v: Awaited<ReturnType<typeof listProductUnits>>) => void = () => {}
    vi.mocked(listProductUnits).mockReturnValue(new Promise((r) => (resolveUnits = r)))
    vi.mocked(getLastPrices).mockResolvedValue({
      items: [
        { product_id: "p1", unit_price: "3.50", unit_name: "件", base_unit_price: "3.50", factor: "1", source: "last", bill_date: "2026-08-02" },
      ],
    })
    render(<Harness partnerId="s1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => screen.getByDisplayValue("3.50"))
    resolveUnits([{ id: "u-base", code: "pcs", name: "件", conversion_factor: "1", is_base: true }])
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-base"))
    expect(screen.getByDisplayValue("3.50")).toBeTruthy()
  })

  const UNITS = [
    { id: "u-base", code: "pcs", name: "件", conversion_factor: "1", is_base: true },
    { id: "u-box", code: "box", name: "箱", conversion_factor: "12", is_base: false },
  ]
  const BOX_HIT = { product_id: "p1", unit_price: "42.00", unit_id: "u-box", unit_name: "箱", base_unit_price: "3.50", factor: "12", source: "last" as const, bill_date: "2026-10-02" }
  const DEFAULT_HIT = { product_id: "p1", unit_price: "3.00", unit_name: "件", base_unit_price: "3.00", factor: "1", source: "default" as const, bill_date: "" }

  it("a carton-quoting supplier then one without history: the line is back on the base unit at 3.00", async () => {
    vi.mocked(listProductUnits).mockResolvedValue(UNITS)
    vi.mocked(getLastPrices).mockImplementation(async (p) => ({ items: [p.partnerId === "s1" ? BOX_HIT : DEFAULT_HIT] }))
    const { rerender } = render(<Harness partnerId="s1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-box"))
    rerender(<Harness partnerId="s2" />)
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-base"))
    expect(screen.getByDisplayValue("3.00")).toBeTruthy()
    expect(screen.getByText("3.0000", { selector: "td" })).toBeTruthy()
  })

  it("re-picking a product forgets the old product's unit, so clearing the supplier cannot restore it", async () => {
    vi.mocked(listProductUnits).mockResolvedValue(UNITS)
    vi.mocked(getLastPrices).mockResolvedValue({ items: [BOX_HIT] })
    const { rerender } = render(<Harness partnerId="s1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-box"))
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-box"))
    vi.mocked(getLastPrices).mockResolvedValue({ items: [] })
    rerender(<Harness partnerId="s9" />)
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("u-base"))
  })

  it("changing the unit by hand reprices an auto-filled price by the unit's factor", async () => {
    vi.mocked(listProductUnits).mockResolvedValue(UNITS)
    vi.mocked(getLastPrices).mockResolvedValue({ items: [BOX_HIT] })
    render(<Harness partnerId="s1" />)
    fireEvent.click(screen.getByText("pick-product"))
    await waitFor(() => expect(screen.getByDisplayValue("42.00")).toBeTruthy())
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "u-base" } })
    expect(screen.getByDisplayValue("3.50")).toBeTruthy()
  })
})
