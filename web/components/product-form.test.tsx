import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { ProductForm } from "./product-form"

vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: "retail" }),
  ProfileGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))
vi.mock("@/components/unit-selector", () => ({
  UnitSelector: () => <select data-testid="unit-selector" />,
}))
vi.mock("@/components/cross-border/hs-code-input", () => ({
  HsCodeInput: () => <input data-testid="hs-code-input" />,
}))

// Role under test; the purchase price box is drawn only for people who may see costs.
const { roleState } = vi.hoisted(() => ({ roleState: { role: "owner" as string | null } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: roleState.role ? { role: roleState.role } : {} } }),
}))

const mockListProducts = vi.fn()
vi.mock("@/lib/api/products", () => ({
  listProducts: (...args: unknown[]) => mockListProducts(...args),
}))
beforeEach(() => {
  roleState.role = "owner"
  mockListProducts.mockReset()
  mockListProducts.mockResolvedValue({ items: [], total: 0 })
})

describe("ProductForm prices", () => {
  it("TestProductForm_PricesSubmitted: filled prices go out as strings, blanks are omitted", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<ProductForm onSubmit={onSubmit} />)

    fireEvent.change(screen.getByLabelText(/商品名称/), { target: { value: "螺丝" } })
    fireEvent.change(screen.getByLabelText("零售价"), { target: { value: "12.50" } })
    fireEvent.change(screen.getByLabelText("进价"), { target: { value: "8" } })
    fireEvent.click(screen.getByRole("button", { name: "保存" }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    const input = onSubmit.mock.calls[0][0]
    expect(input.retail_price).toBe("12.50")
    expect(input.purchase_price).toBe("8")
    expect(input.wholesale_price).toBeUndefined()
  })

  it("TestProductForm_PriceRejectsLettersAndNegative", () => {
    render(<ProductForm onSubmit={vi.fn()} />)
    const retail = screen.getByLabelText("零售价") as HTMLInputElement
    fireEvent.change(retail, { target: { value: "abc" } })
    expect(retail.value).toBe("")
    fireEvent.change(retail, { target: { value: "-3" } })
    expect(retail.value).toBe("")
  })

  it("TestProductForm_EditPrefillsPrices_ZeroShownBlank", () => {
    render(
      <ProductForm
        onSubmit={vi.fn()}
        initial={{ name: "x", retail_price: "13.75", wholesale_price: "0", purchase_price: "8" }}
      />,
    )
    expect((screen.getByLabelText("零售价") as HTMLInputElement).value).toBe("13.75")
    expect((screen.getByLabelText("批发价") as HTMLInputElement).value).toBe("")
    expect((screen.getByLabelText("进价") as HTMLInputElement).value).toBe("8")
    expect(screen.getByText(/留空 = 收银时手输/)).toBeInTheDocument()
  })

  it.each(["clerk", null])("TestProductForm_NoPurchasePriceBoxFor_%s", (role) => {
    roleState.role = role
    render(<ProductForm onSubmit={vi.fn()} />)
    expect(screen.queryByLabelText("进价")).toBeNull()
  })

  it("TestProductForm_ClerkSeesWhoFillsPurchasePrice", () => {
    roleState.role = "clerk"
    render(<ProductForm onSubmit={vi.fn()} />)
    expect(screen.getByText("进价由老板或店长填写。")).toBeInTheDocument()
  })

  it("TestProductForm_OwnerSeesNoPurchasePriceNote", () => {
    render(<ProductForm onSubmit={vi.fn()} />)
    expect(screen.queryByText("进价由老板或店长填写。")).toBeNull()
  })
})

describe("ProductForm barcode", () => {
  it("TestProductForm_BarcodeSubmittedInAttributes", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<ProductForm onSubmit={onSubmit} />)

    fireEvent.change(screen.getByLabelText(/商品名称/), { target: { value: "螺丝" } })
    fireEvent.change(screen.getByLabelText("条码"), { target: { value: " 6901234567890 " } })
    fireEvent.click(screen.getByRole("button", { name: "保存" }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    expect(onSubmit.mock.calls[0][0].attributes.barcode).toBe("6901234567890")
  })

  it("TestProductForm_BarcodeEnter_DoesNotSubmit", () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<ProductForm onSubmit={onSubmit} />)
    const bc = screen.getByLabelText("条码")
    const notPrevented = fireEvent.keyDown(bc, { key: "Enter" })
    expect(notPrevented).toBe(false)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it("TestProductForm_DuplicateBarcode_BlocksWithMessage", async () => {
    mockListProducts.mockResolvedValue({ items: [{ id: "other", name: "老螺丝" }], total: 1 })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<ProductForm onSubmit={onSubmit} />)
    fireEvent.change(screen.getByLabelText(/商品名称/), { target: { value: "螺丝" } })
    fireEvent.change(screen.getByLabelText("条码"), { target: { value: "6901" } })
    fireEvent.click(screen.getByRole("button", { name: "保存" }))
    expect(await screen.findByText(/条码已被「老螺丝」使用/)).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it("TestProductForm_EditOwnBarcode_NotDuplicate", async () => {
    mockListProducts.mockResolvedValue({ items: [{ id: "me", name: "自己" }], total: 1 })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<ProductForm onSubmit={onSubmit} initial={{ id: "me", name: "x", attributes: { barcode: "111" } }} />)
    fireEvent.click(screen.getByRole("button", { name: "保存" }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
  })

  it("TestProductForm_EditKeepsOtherAttributes_AndClearingBarcodeSendsNull", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <ProductForm
        onSubmit={onSubmit}
        initial={{ name: "x", attributes: { barcode: "111", latin_name: "Ficus", is_bulk: true } }}
      />,
    )
    expect((screen.getByLabelText("条码") as HTMLInputElement).value).toBe("111")

    fireEvent.click(screen.getByRole("button", { name: "保存" }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    expect(onSubmit.mock.calls[0][0].attributes).toMatchObject({ barcode: "111", latin_name: "Ficus", is_bulk: true })

    fireEvent.change(screen.getByLabelText("条码"), { target: { value: "" } })
    fireEvent.click(screen.getByRole("button", { name: "保存" }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2))
    const attrs = onSubmit.mock.calls[1][0].attributes
    expect(attrs.barcode).toBeNull()
    expect(attrs.latin_name).toBe("Ficus")
  })

  it("TestProductForm_SubmitError_NoClassNamePrefix", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("编码已存在"))
    render(<ProductForm onSubmit={onSubmit} />)
    fireEvent.change(screen.getByLabelText(/商品名称/), { target: { value: "螺丝" } })
    fireEvent.click(screen.getByRole("button", { name: "保存" }))
    expect(await screen.findByText("编码已存在")).toBeInTheDocument()
  })
})

describe("ProductForm purchase price by role", () => {
  it("manager sees the purchase price box", () => {
    roleState.role = "manager"
    render(<ProductForm onSubmit={vi.fn()} />)
    expect(screen.getByLabelText("进价")).toBeInTheDocument()
  })

  it("an unknown role sees no purchase price box and sends none", async () => {
    roleState.role = null
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<ProductForm onSubmit={onSubmit} initial={{ id: "p1", name: "螺丝", purchase_price: "8" } as never} />)
    expect(screen.queryByLabelText("进价")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "保存" }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    expect(onSubmit.mock.calls[0][0].purchase_price).toBeUndefined()
  })
})
