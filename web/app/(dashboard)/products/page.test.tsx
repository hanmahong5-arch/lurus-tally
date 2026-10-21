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

// 扫码按钮：摄像头相关能力在测试里不可用，换成一个会回调条码的按钮。
vi.mock("@/components/barcode-scanner", () => ({
  BarcodeScanner: ({ onScan }: { onScan: (c: string) => void }) => (
    <button type="button" onClick={() => onScan("6901234567890")}>
      扫码
    </button>
  ),
}))

// Mock the products API
vi.mock("@/lib/api/products", () => ({
  listProducts: vi.fn(),
  deleteProduct: vi.fn(),
  restoreProduct: vi.fn(),
}))

// Shop role of the signed-in person; null means the session has not told us yet.
const roleState = vi.hoisted(() => ({ role: null as string | null }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({
    data: roleState.role ? { user: { role: roleState.role, isOwner: roleState.role === "owner" } } : null,
  }),
}))

import { listProducts, deleteProduct, restoreProduct } from "@/lib/api/products"
import ProductsPage from "./page"
import { roleCan, type ShopRole } from "@/lib/roles"

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
    roleState.role = null
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

describe("ProductsPage ?import=1", () => {
  it("opens the import dialog once and drops the flag from the address", async () => {
    vi.mocked(listProducts).mockResolvedValue({ items: [mockProduct], total: 1 })
    window.history.replaceState(null, "", "/products?import=1&x=2")
    render(<ProductsPage />)
    await waitFor(() => expect(window.location.search).toBe("?x=2"))
    window.history.replaceState(null, "", "/")
  })
})

describe("ProductsPage by role", () => {
  beforeEach(() => {
    vi.mocked(listProducts).mockResolvedValue({ items: [mockProduct], total: 1 })
  })

  it("lets a clerk add, import, edit and delete products", async () => {
    roleState.role = "clerk"
    render(<ProductsPage />)
    await screen.findByText("测试商品")
    expect(screen.getByRole("link", { name: "+ 新建商品" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "批量导入" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument()
  })

  it.each(["cashier", "finance"])("leaves a %s only the list", async (role) => {
    roleState.role = role
    render(<ProductsPage />)
    await screen.findByText("测试商品")
    expect(screen.queryByRole("link", { name: "+ 新建商品" })).toBeNull()
    expect(screen.queryByRole("button", { name: "批量导入" })).toBeNull()
    expect(screen.queryByRole("button", { name: "删除" })).toBeNull()
    expect(screen.queryByText("编辑")).toBeNull()
  })
})

describe("ProductsPage entries match the role's permissions", () => {
  it.each(["owner", "manager", "clerk", "cashier", "finance"] as const)("%s sees only the entries the backend accepts", async (role: ShopRole) => {
    roleState.role = role
    vi.mocked(listProducts).mockResolvedValue({ items: [mockProduct], total: 1 })
    render(<ProductsPage />)
    await screen.findByText("测试商品")
    const write = roleCan(role, "product.write")
    expect(!!screen.queryByRole("link", { name: "+ 新建商品" })).toBe(write)
    expect(!!screen.queryByRole("button", { name: "批量导入" })).toBe(write)
    expect(!!screen.queryByRole("button", { name: "删除" })).toBe(write)
    expect(!!screen.queryByText("编辑")).toBe(write)
  })
})

describe("ProductsPage 扫码", () => {
  it("搜索框旁有扫码按钮，扫到条码即填入并查询", async () => {
    vi.mocked(listProducts).mockResolvedValue({ items: [], total: 0 } as never)
    render(<ProductsPage />)
    fireEvent.click(await screen.findByRole("button", { name: "扫码" }))
    await waitFor(() =>
      expect(vi.mocked(listProducts)).toHaveBeenCalledWith(expect.objectContaining({ q: "6901234567890" })),
    )
    expect((screen.getByLabelText("搜索商品") as HTMLInputElement).value).toBe("6901234567890")
  })
})
