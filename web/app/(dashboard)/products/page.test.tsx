import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { globalUndoStack } from "@/lib/undo/undo-stack"

// Mock next/link
vi.mock("next/link", () => ({
  default: ({
    href,
    children,
    ...props
  }: {
    href: string
    children: React.ReactNode
    [key: string]: unknown
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))

// Mock the products API
vi.mock("@/lib/api/products", () => ({
  listProducts: vi.fn(),
  deleteProduct: vi.fn(),
  restoreProduct: vi.fn(),
}))

import { listProducts, deleteProduct, restoreProduct } from "@/lib/api/products"
import ProductsPage from "./page"

const mockProduct = {
  id: "prod-abc-123",
  tenant_id: "tenant-1",
  code: "P001",
  name: "测试商品",
  brand: "Brand",
  measurement_strategy: "individual" as const,
  enabled: true,
  enable_serial_no: false,
  enable_lot_no: false,
  attributes: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
}

describe("ProductsPage undo-aware delete", () => {
  beforeEach(() => {
    globalUndoStack.resetForTest()
    vi.mocked(listProducts).mockResolvedValue({ items: [mockProduct], total: 1 })
    vi.mocked(deleteProduct).mockResolvedValue(undefined)
    vi.mocked(restoreProduct).mockResolvedValue(undefined)
  })

  it("TestProductList_Delete_PushesUndoStack", async () => {
    render(<ProductsPage />)

    // Wait for product list to render
    await waitFor(() => {
      expect(screen.getByText("测试商品")).toBeInTheDocument()
    })

    expect(globalUndoStack.size()).toBe(0)

    // Click delete button
    fireEvent.click(screen.getByRole("button", { name: "删除" }))

    // Undo entry should be pushed before delete completes
    expect(globalUndoStack.size()).toBe(1)

    await waitFor(() => {
      expect(deleteProduct).toHaveBeenCalledWith("prod-abc-123", undefined)
    })
  })

  it("TestProductList_Undo_CallsRestoreEndpoint", async () => {
    render(<ProductsPage />)

    await waitFor(() => {
      expect(screen.getByText("测试商品")).toBeInTheDocument()
    })

    // Trigger delete to push undo entry
    fireEvent.click(screen.getByRole("button", { name: "删除" }))

    await waitFor(() => {
      expect(globalUndoStack.size()).toBe(1)
    })

    const entry = globalUndoStack.pop()
    expect(entry).toBeDefined()

    // Trigger the revert closure
    await entry!.action.revert()

    expect(restoreProduct).toHaveBeenCalledWith("prod-abc-123", undefined)
  })
})
