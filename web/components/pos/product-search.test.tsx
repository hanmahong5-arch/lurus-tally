import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react"
import { ProductSearch } from "./product-search"
import type { Product } from "@/lib/api/products"

const mockListProducts = vi.fn()
vi.mock("@/lib/api/products", () => ({
  listProducts: (...args: unknown[]) => mockListProducts(...args),
}))

const makeProduct = (id: string, name: string): Product => ({
  id,
  tenant_id: "t1",
  code: `CODE-${id}`,
  name,
  enabled: true,
  enable_serial_no: false,
  enable_lot_no: false,
  measurement_strategy: "individual",
  attributes: {},
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
})

beforeEach(() => {
  mockListProducts.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe("ProductSearch", () => {
  it("TestProductSearch_NumericInput_TriggersBarcodeLookup: numeric input uses attribute_filter", async () => {
    mockListProducts.mockResolvedValue({ items: [], total: 0 })
    const onSelect = vi.fn()

    render(<ProductSearch onSelect={onSelect} tenantId="t1" />)
    const input = screen.getByRole("textbox")

    await act(async () => {
      fireEvent.change(input, { target: { value: "12345678" } })
      // Wait for debounce + async
      await new Promise((r) => setTimeout(r, 300))
    })

    expect(mockListProducts).toHaveBeenCalledWith(
      expect.objectContaining({
        attributes_filter: { barcode: "12345678" },
        limit: 1,
      })
    )
  })

  it("TestProductSearch_ChineseInput_TriggersNameSearch: text input uses q param", async () => {
    mockListProducts.mockResolvedValue({ items: [], total: 0 })
    const onSelect = vi.fn()

    render(<ProductSearch onSelect={onSelect} tenantId="t1" />)
    const input = screen.getByRole("textbox")

    await act(async () => {
      fireEvent.change(input, { target: { value: "螺丝" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    expect(mockListProducts).toHaveBeenCalledWith(
      expect.objectContaining({ q: "螺丝", limit: 20 })
    )
  })

  it("TestProductSearch_SelectItem_CallsOnSelect: clicking dropdown item triggers onSelect", async () => {
    const product = makeProduct("p1", "Bolt M8")
    mockListProducts.mockResolvedValue({ items: [product], total: 1 })
    const onSelect = vi.fn()

    render(<ProductSearch onSelect={onSelect} tenantId="t1" />)
    const input = screen.getByRole("textbox")

    await act(async () => {
      fireEvent.change(input, { target: { value: "bolt" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    await waitFor(() => {
      expect(screen.getByText("Bolt M8")).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText("Bolt M8"))
    expect(onSelect).toHaveBeenCalledWith(product)
  })

  it("TestProductSearch_EmptyInput_ClearsResults: clearing input removes dropdown", async () => {
    const product = makeProduct("p1", "Wrench")
    mockListProducts.mockResolvedValue({ items: [product], total: 1 })
    const onSelect = vi.fn()

    render(<ProductSearch onSelect={onSelect} tenantId="t1" />)
    const input = screen.getByRole("textbox")

    await act(async () => {
      fireEvent.change(input, { target: { value: "wrench" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    await waitFor(() => expect(screen.queryByText("Wrench")).toBeInTheDocument())

    await act(async () => {
      fireEvent.change(input, { target: { value: "" } })
    })

    await waitFor(() => expect(screen.queryByText("Wrench")).not.toBeInTheDocument())
  })

  it("TestProductSearch_BarcodeUniqueMatch_AutoAddsToCart: single barcode match calls onSelect automatically", async () => {
    const product = makeProduct("p1", "Bolt M8")
    mockListProducts.mockResolvedValue({ items: [product], total: 1 })
    const onSelect = vi.fn()

    render(<ProductSearch onSelect={onSelect} tenantId="t1" />)
    const input = screen.getByRole("textbox")

    await act(async () => {
      fireEvent.change(input, { target: { value: "1234567890" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    expect(onSelect).toHaveBeenCalledWith(product)
  })
})
