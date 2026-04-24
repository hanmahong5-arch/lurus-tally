import { describe, it, expect, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { ProductGrid } from "./product-grid"
import type { Product } from "@/lib/api/products"

const makeProduct = (id: string, name: string, isCommon = false): Product => ({
  id,
  tenant_id: "t1",
  code: `CODE-${id}`,
  name,
  enabled: true,
  enable_serial_no: false,
  enable_lot_no: false,
  measurement_strategy: "individual",
  attributes: isCommon ? { is_common: true } : {},
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
})

describe("ProductGrid", () => {
  it("TestProductGrid_ClickCard_CallsOnAdd: clicking a product card calls onAdd with that product", () => {
    const products = [makeProduct("p1", "Bolt M8"), makeProduct("p2", "Nut M8")]
    const onAdd = vi.fn()

    render(<ProductGrid products={products} onAdd={onAdd} />)

    fireEvent.click(screen.getByText("Bolt M8"))
    expect(onAdd).toHaveBeenCalledWith(products[0])
    expect(onAdd).toHaveBeenCalledTimes(1)
  })

  it("TestProductGrid_CategoryFilter_ShowsOnlyMatch: 常用 tab shows only is_common products", () => {
    const products = [
      makeProduct("p1", "Common Bolt", true),
      makeProduct("p2", "Regular Nut", false),
    ]
    const onAdd = vi.fn()

    render(<ProductGrid products={products} onAdd={onAdd} />)

    // Click the 常用 tab
    fireEvent.click(screen.getByText("常用"))

    expect(screen.getByText("Common Bolt")).toBeInTheDocument()
    expect(screen.queryByText("Regular Nut")).not.toBeInTheDocument()
  })

  it("TestProductGrid_AllTab_ShowsAllProducts: 全部 tab shows all products", () => {
    const products = [
      makeProduct("p1", "Product A", true),
      makeProduct("p2", "Product B", false),
    ]
    const onAdd = vi.fn()

    render(<ProductGrid products={products} onAdd={onAdd} />)

    // Default is 全部 tab
    expect(screen.getByText("Product A")).toBeInTheDocument()
    expect(screen.getByText("Product B")).toBeInTheDocument()
  })

  it("TestProductGrid_EmptyProducts_ShowsEmptyState", () => {
    const onAdd = vi.fn()
    render(<ProductGrid products={[]} onAdd={onAdd} />)
    expect(screen.getByText(/暂无商品/)).toBeInTheDocument()
  })

  it("TestProductGrid_EmptyCommonTab_ShowsEmptyMessage: no common products shows placeholder", () => {
    const products = [makeProduct("p1", "Regular Only", false)]
    const onAdd = vi.fn()

    render(<ProductGrid products={products} onAdd={onAdd} />)
    fireEvent.click(screen.getByText("常用"))

    expect(screen.getByText(/暂无常用商品/)).toBeInTheDocument()
  })
})
