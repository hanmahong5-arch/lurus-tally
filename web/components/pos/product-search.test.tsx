import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react"
import { ProductSearch } from "./product-search"
import type { Product } from "@/lib/api/products"
import { NetworkError } from "@/lib/api/errors"

const mockListProducts = vi.fn()
vi.mock("@/lib/api/products", () => ({
  listProducts: (...args: unknown[]) => mockListProducts(...args),
}))

const mockSearchCache = vi.fn()
vi.mock("@/lib/catalog-cache", async () => {
  const actual = await vi.importActual<typeof import("@/lib/catalog-cache")>("@/lib/catalog-cache")
  return { ...actual, searchCatalogWithMeta: (...a: unknown[]) => mockSearchCache(...a) }
})

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
        limit: 2,
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
      fireEvent.change(input, { target: { value: "wr" } })
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

  it("TestProductSearch_BarcodeSharedByTwo_ShowsListInsteadOfGuessing", async () => {
    const a = makeProduct("p1", "螺丝甲")
    const b = makeProduct("p2", "螺丝乙")
    mockListProducts.mockResolvedValue({ items: [a, b], total: 2 })
    const onSelect = vi.fn()
    render(<ProductSearch onSelect={onSelect} tenantId="t1" />)

    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "1234567890" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    expect(mockListProducts).toHaveBeenCalledWith(expect.objectContaining({ attributes_filter: { barcode: "1234567890" }, limit: 2 }))
    expect(onSelect).not.toHaveBeenCalled()
    expect(screen.getByText("螺丝甲")).toBeInTheDocument()
    expect(screen.getByText("螺丝乙")).toBeInTheDocument()
  })

  it("TestProductSearch_AlphanumericBarcode_UsesAttributeFilter: 6+ 位字母数字走条码精确查", async () => {
    mockListProducts.mockResolvedValue({ items: [], total: 0 })
    render(<ProductSearch onSelect={vi.fn()} tenantId="t1" />)

    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "ABC-123456" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    expect(mockListProducts).toHaveBeenCalledWith(
      expect.objectContaining({ attributes_filter: { barcode: "ABC-123456" }, limit: 2 }),
    )
  })

  it("TestProductSearch_ShortInput_SkipsBarcodeLookup: 少于 6 位(如规格 304)直接按名称/编码搜", async () => {
    const product = makeProduct("p304", "不锈钢螺丝 304")
    mockListProducts.mockResolvedValue({ items: [product], total: 1 })
    render(<ProductSearch onSelect={vi.fn()} tenantId="t1" />)

    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "304" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    expect(mockListProducts).not.toHaveBeenCalledWith(expect.objectContaining({ attributes_filter: expect.anything() }))
    expect(mockListProducts).toHaveBeenCalledWith(expect.objectContaining({ q: "304", limit: 20 }))
    await waitFor(() => expect(screen.getByText("不锈钢螺丝 304")).toBeInTheDocument())
  })

  it("TestProductSearch_BarcodeMiss_FallsBackToNameSearch: 条码无命中回落 q 搜索", async () => {
    const product = makeProduct("p1", "螺丝 SK304X")
    mockListProducts.mockImplementation((params: { attributes_filter?: unknown }) =>
      Promise.resolve(params.attributes_filter ? { items: [], total: 0 } : { items: [product], total: 1 }),
    )
    render(<ProductSearch onSelect={vi.fn()} tenantId="t1" />)

    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "SK304X" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    expect(mockListProducts).toHaveBeenCalledWith(expect.objectContaining({ attributes_filter: { barcode: "SK304X" } }))
    expect(mockListProducts).toHaveBeenCalledWith(expect.objectContaining({ q: "SK304X", limit: 20 }))
    await waitFor(() => expect(screen.getByText("螺丝 SK304X")).toBeInTheDocument())
  })

  it("TestProductSearch_NumericNoMatchAnywhere_ShowsNotFound", async () => {
    mockListProducts.mockResolvedValue({ items: [], total: 0 })
    render(<ProductSearch onSelect={vi.fn()} tenantId="t1" />)

    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "999" } })
      await new Promise((r) => setTimeout(r, 300))
    })

    await waitFor(() => expect(screen.getByText("未找到商品")).toBeInTheDocument())
  })
  it("TestProductSearch_NetworkError_FallsBackToCacheAndLabelsPrice", async () => {
    mockListProducts.mockRejectedValue(new NetworkError("network", "x", false))
    mockSearchCache.mockResolvedValue({
      items: [{ id: "c1", name: "缓存螺丝", code: "C1", barcode: "", sale_price: "3.50", unit: "个" }],
      updated_at: Date.now() - 5 * 60_000,
    })
    render(<ProductSearch onSelect={vi.fn()} tenantId="t1" />)
    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "螺丝" } })
      await new Promise((r) => setTimeout(r, 300))
    })
    await waitFor(() => expect(screen.getByText("缓存螺丝")).toBeInTheDocument())
    expect(screen.getByText(/缓存价（5 分钟前）/)).toBeInTheDocument()
  })

  it("TestProductSearch_Offline_UniqueBarcodeHitAutoAdds", async () => {
    const online = vi.spyOn(window.navigator, "onLine", "get").mockReturnValue(false)
    mockSearchCache.mockResolvedValue({
      items: [{ id: "c2", name: "缓存商品", code: "C2", barcode: "6901234567", sale_price: "9.00", unit: "个" }],
      updated_at: Date.now(),
    })
    const onSelect = vi.fn()
    render(<ProductSearch onSelect={onSelect} tenantId="t1" />)
    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "6901234567" } })
      await new Promise((r) => setTimeout(r, 300))
    })
    await waitFor(() => expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ id: "c2", retail_price: "9.00" })))
    expect(mockListProducts).not.toHaveBeenCalled()
    online.mockRestore()
  })
})
