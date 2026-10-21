/**
 * Tests for the bill-form pickers' "create hits an existing record" path.
 */
import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { ApiError } from "@/lib/api/errors"
import { CustomerPicker, SupplierPicker } from "./entity-pickers"

const toastMock = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }))
vi.mock("sonner", () => ({ toast: toastMock }))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))

const customers = vi.hoisted(() => ({
  listCustomers: vi.fn(),
  getCustomer: vi.fn(),
  createCustomer: vi.fn(),
}))
vi.mock("@/lib/api/customers", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/customers")>()),
  ...customers,
}))

const suppliers = vi.hoisted(() => ({ listSuppliers: vi.fn(), createSupplier: vi.fn() }))
vi.mock("@/lib/api/suppliers", () => suppliers)
vi.mock("@/lib/api/products", () => ({ listProducts: vi.fn(), createProduct: vi.fn() }))

function pick(Picker: typeof CustomerPicker, text: string) {
  const onSelect = vi.fn()
  render(<Picker value="" label="" onSelect={onSelect} />)
  fireEvent.change(screen.getByRole("combobox"), { target: { value: text } })
  return { onSelect }
}

describe("pickers: create hitting an existing record", () => {
  afterEach(() => vi.clearAllMocks())

  it("customer 409 selects the existing profile and says so", async () => {
    customers.listCustomers.mockResolvedValue({ items: [], total: 0 })
    customers.createCustomer.mockRejectedValue(
      new ApiError(409, "duplicate_customer_name", "已存在同名客户「王五」", { existing_id: "c-9" })
    )
    customers.getCustomer.mockResolvedValue({ id: "c-9", name: "王五" })
    const { onSelect } = pick(CustomerPicker, "王五 ")
    fireEvent.mouseDown(await screen.findByText(/新建客户「王五」/))
    await waitFor(() => expect(onSelect).toHaveBeenCalledWith({ id: "c-9", name: "王五", reused: true }))
    expect(customers.getCustomer).toHaveBeenCalledWith("c-9", "t-1")
    expect(toastMock.info).toHaveBeenCalledWith("已存在同名客户，已为你选中")
    expect(toastMock.success).not.toHaveBeenCalled()
  })

  it("customer non-409 errors still surface", async () => {
    customers.listCustomers.mockResolvedValue({ items: [], total: 0 })
    customers.createCustomer.mockRejectedValue(new ApiError(400, "bad", "名称无效"))
    pick(CustomerPicker, "王五")
    fireEvent.mouseDown(await screen.findByText(/新建客户「王五」/))
    expect(await screen.findByText(/创建失败：名称无效/)).toBeTruthy()
  })

  it("supplier 409 looks the name up and selects the match", async () => {
    suppliers.listSuppliers.mockResolvedValue({ items: [{ id: "s-3", name: "Acme" }], total: 1 })
    suppliers.createSupplier.mockRejectedValue(new ApiError(409, "duplicate supplier name", "duplicate supplier name", {}))
    const { onSelect } = pick(SupplierPicker as typeof CustomerPicker, "acme")
    // First search returns the same row as a non-exact hit only when names differ; force the create row.
    suppliers.listSuppliers.mockResolvedValueOnce({ items: [], total: 0 })
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "acme " } })
    fireEvent.mouseDown(await screen.findByText(/新建供应商「acme」/))
    await waitFor(() => expect(onSelect).toHaveBeenCalledWith({ id: "s-3", name: "Acme", reused: true }))
    expect(toastMock.info).toHaveBeenCalledWith("已存在同名供应商，已为你选中")
  })

  it("supplier 409 with no match shows a readable message", async () => {
    suppliers.listSuppliers.mockResolvedValue({ items: [], total: 0 })
    suppliers.createSupplier.mockRejectedValue(new ApiError(409, "duplicate supplier name", "duplicate supplier name", {}))
    pick(SupplierPicker as typeof CustomerPicker, "Acme")
    fireEvent.mouseDown(await screen.findByText(/新建供应商「Acme」/))
    expect(await screen.findByText(/已存在同名供应商/)).toBeTruthy()
  })
})
