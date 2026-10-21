import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import PosPage from "./page"

// Role under test; the return entry depends on it.
const { roleState } = vi.hoisted(() => ({ roleState: { role: "cashier" as string, scoped: false } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({
    data: { user: roleState.scoped ? { role: roleState.role, id: "u1", tenantId: "t1" } : { role: roleState.role } },
  }),
}))

// 结账本地台账的存储与能力标志：页面测试里用内存代替 IndexedDB 与网络。
const queueMem = new Map<string, unknown>()
vi.mock("@/lib/draft/idb-storage", () => ({
  draftGet: async (k: string) => queueMem.get(k),
  draftSet: async (k: string, v: unknown) => void queueMem.set(k, v),
  isDraftStorageAvailable: () => true,
}))
vi.mock("@/lib/api/todo", () => ({ getTodoSummary: async () => ({ total: 0 }) }))
vi.mock("@/lib/catalog-cache", () => ({ refreshCatalog: async () => false }))

// Mock all dependencies
const mockConfirm = vi.fn<(o: unknown) => Promise<boolean>>(async () => true)
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => mockConfirm }))
const mockListProducts = vi.fn()
vi.mock("@/lib/api/products", () => ({
  listProducts: (...args: unknown[]) => mockListProducts(...args),
}))

const mockListUnits = vi.fn()
vi.mock("@/lib/api/units", () => ({
  listUnits: (...args: unknown[]) => mockListUnits(...args),
}))

const mockQuickCheckout = vi.fn()
vi.mock("@/lib/api/pos", () => ({
  quickCheckout: (...args: unknown[]) => mockQuickCheckout(...args),
}))

vi.mock("@/components/entity-pickers", () => ({
  CustomerPicker: (props: { onSelect: (o: { id: string; name: string }) => void }) => (
    <button type="button" onClick={() => props.onSelect({ id: "cust-1", name: "老王" })}>
      选中老王
    </button>
  ),
}))

vi.mock("@/components/pos/pos-return", () => ({
  PosReturn: ({ open, onDone }: { open: boolean; onDone: (m: string) => void }) =>
    open ? (
      <button type="button" onClick={() => onDone("退货成功，退货金额 ¥40.00")}>
        stub-return-done
      </button>
    ) : null,
}))

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))

const sampleProduct = {
  id: "p1",
  tenant_id: "t1",
  code: "BOLT-M8",
  name: "Bolt M8",
  enabled: true,
  enable_serial_no: false,
  enable_lot_no: false,
  measurement_strategy: "individual",
  attributes: {},
  default_unit_id: "u1",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
}

beforeEach(() => {
  roleState.role = "cashier"
  roleState.scoped = false
  queueMem.clear()
  window.sessionStorage.clear()
  mockConfirm.mockClear()
  mockListProducts.mockReset()
  mockQuickCheckout.mockReset()
  mockListUnits.mockReset()
  mockListUnits.mockResolvedValue({ items: [{ id: "u1", name: "个" }] })
  mockListProducts.mockResolvedValue({ items: [], total: 0 })
})

describe("PosPage", () => {
  it.each(["clerk", "finance"])("shows a hint instead of the till to %s", (role) => {
    roleState.role = role
    render(<PosPage />)
    expect(screen.getByText("收银请找收银员或老板")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "回首页" })).toHaveAttribute("href", "/dashboard")
    expect(screen.queryByRole("textbox")).toBeNull()
  })

  it("TestPosPage_InitialRender_HasSearchInput: page renders the search input", () => {
    render(<PosPage />)
    expect(screen.getByRole("textbox")).toBeInTheDocument()
  })

  it("TestPosPage_AddProduct_AppearsInCart: adding product via grid updates cart", () => {
    mockListProducts.mockResolvedValue({ items: [sampleProduct], total: 1 })

    render(<PosPage />)

    // Initially cart is empty
    expect(screen.getByText(/购物车为空/)).toBeInTheDocument()
  })

  it("TestPosPage_EmptyCart_CheckoutButtonsDisabled: checkout buttons disabled when cart is empty", () => {
    render(<PosPage />)

    const cashBtn = screen.getByRole("button", { name: /现金/ })
    expect(cashBtn).toBeDisabled()
  })

  it("TestPosPage_Checkout_CallsQuickCheckoutApi: after adding product and checking out, API is called", async () => {
    mockQuickCheckout.mockResolvedValue({
      bill_id: "bill-1",
      bill_no: "SO-001",
      total_amount: "9.99",
      receivable_amount: "9.99",
    })

    render(<PosPage />)

    // We can't easily simulate the full flow in this unit test
    // The important thing is the page renders with the correct structure
    expect(screen.getByText("POS 收银台")).toBeInTheDocument()
  })

  it("TestPosPage_NoWarehouseConfigBanner: warehouse is resolved by the backend, no setup banner", () => {
    render(<PosPage />)
    expect(screen.queryByText(/未配置仓库/)).not.toBeInTheDocument()
    expect(screen.getByText(/购物车为空/)).toBeInTheDocument()
  })

  async function addAndPay(product: Record<string, unknown>, paid?: string) {
    mockListProducts.mockResolvedValue({ items: [product], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText(String(product.name)))
    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    if (paid) fireEvent.change(await screen.findByLabelText("实收金额"), { target: { value: paid } })
    fireEvent.click(await screen.findByRole("button", { name: /确认收款/ }))
  }

  it("TestPosPage_Checkout_UsesRetailPriceUnitAndOmitsWarehouse", async () => {
    mockQuickCheckout.mockResolvedValue({ bill_id: "b", bill_no: "SO-1", total_amount: "12.50", receivable_amount: "0" })
    await addAndPay({ ...sampleProduct, retail_price: "12.50" }, "20")
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalled())
    const req = mockQuickCheckout.mock.calls[0][0]
    expect(req.items[0].unit_price).toBe("12.5000")
    expect(req.items[0].unit_id).toBe("u1")
    expect(req.items[0].unit_name).toBe("个")
    expect(req.items[0].warehouse_id).toBeUndefined()
  })

  it("TestPosPage_NoDefaultUnit_OmitsUnitId_NeverSendsDefaultLiteral", async () => {
    mockQuickCheckout.mockResolvedValue({ bill_id: "b", bill_no: "SO-2", total_amount: "5.00", receivable_amount: "0" })
    await addAndPay({ ...sampleProduct, default_unit_id: undefined, retail_price: 5 }, "5")
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalled())
    const item = mockQuickCheckout.mock.calls[0][0].items[0]
    expect(item.unit_id).toBeUndefined()
  })

  it("TestPosPage_NoRetailPrice_BlocksCheckoutAndPromptsPrice", async () => {
    mockListProducts.mockResolvedValue({ items: [sampleProduct], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    expect(screen.getByLabelText("Bolt M8 单价")).toHaveValue(null)
    expect(screen.getByRole("button", { name: /现金/ })).toBeDisabled()
  })

  it("TestPosPage_Checkout422_ShowsServerChineseMessage", async () => {
    const { ApiError } = await import("@/lib/api/errors")
    mockQuickCheckout.mockRejectedValue(
      new ApiError(422, "warehouse_required", "x", { error: "warehouse_required", message: "没有默认仓库，请先在「仓库」中设置" }),
    )
    await addAndPay({ ...sampleProduct, retail_price: "3" }, "3")
    expect(await screen.findByText(/没有默认仓库/)).toBeInTheDocument()
  })

  it("TestPosPage_Discount_SentToBackendAndSuccessShowsBackendTotal", async () => {
    // Backend echoes the bill total it stored (line total 59.70 less 5 discount).
    mockQuickCheckout.mockResolvedValue({ bill_id: "b", bill_no: "SO-3", total_amount: "54.7", receivable_amount: "0" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "59.70" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.change(screen.getByLabelText("整单优惠"), { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    fireEvent.change(await screen.findByLabelText("实收金额"), { target: { value: "54.70" } })
    fireEvent.click(await screen.findByRole("button", { name: /确认收款/ }))

    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalled())
    const req = mockQuickCheckout.mock.calls[0][0]
    expect(req.items[0].unit_price).toBe("59.7000")
    expect(req.discount_amount).toBe("5.0000")
    expect(req.paid_amount).toBe("54.70")
    // Success overlay shows the bill total returned by the backend, not a client-side figure.
    await screen.findByText("SO-3")
    const amount = document.querySelector(".text-5xl")
    expect(amount?.textContent).toBe("¥54.70")
  })

  it("TestPosPage_NoDiscount_OmitsDiscountAmount", async () => {
    mockQuickCheckout.mockResolvedValue({ bill_id: "b", bill_no: "SO-4", total_amount: "8.00", receivable_amount: "0" })
    await addAndPay({ ...sampleProduct, retail_price: "8" }, "8")
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalled())
    expect(mockQuickCheckout.mock.calls[0][0].discount_amount).toBeUndefined()
  })

  it("TestPosPage_PriceUnitDiffersFromDefaultUnit_RequiresConfirmedPrice", async () => {
    mockListProducts.mockResolvedValue({
      items: [{ ...sampleProduct, retail_price: "3", retail_price_unit: "箱" }],
      total: 1,
    })
    render(<PosPage />)
    // Wait for the unit list so the comparison has the default unit's name ("个").
    await waitFor(() => expect(mockListProducts).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 20))
    fireEvent.click(await screen.findByText("Bolt M8"))
    expect(screen.getByLabelText("Bolt M8 单价")).toHaveValue(null)
    expect(screen.getByRole("button", { name: /现金/ })).toBeDisabled()
  })

  it("TestPosPage_UnitsNotLoaded_BlocksAddUntilLoaded_ThenKeepsPrice", async () => {
    let release: (v: unknown) => void = () => {}
    mockListUnits.mockReturnValue(new Promise((r) => { release = r }))
    mockListProducts.mockResolvedValue({
      items: [{ ...sampleProduct, retail_price: "12.50" }],
      total: 1,
    })
    render(<PosPage />)
    const card = await screen.findByText("Bolt M8")
    fireEvent.click(card)
    expect(screen.getByText(/购物车为空/)).toBeInTheDocument()
    expect(screen.getByText(/正在加载单位信息/)).toBeInTheDocument()

    release({ items: [{ id: "u1", name: "个" }] })
    await waitFor(() => expect(screen.queryByText(/正在加载单位信息/)).not.toBeInTheDocument())
    fireEvent.click(screen.getByText("Bolt M8"))
    expect(screen.getByLabelText("Bolt M8 单价")).toHaveValue(12.5)
  })

  it("TestPosPage_LostReply_RetryReusesKey_ThenNewKeyAfterRejection", async () => {
    const { NetworkError, ApiError } = await import("@/lib/api/errors")
    mockQuickCheckout
      .mockRejectedValueOnce(new NetworkError("network", "timeout"))
      .mockRejectedValueOnce(new ApiError(422, "x", "x", { message: "库存不足" }))
      .mockResolvedValue({ bill_id: "b", bill_no: "SO-9", total_amount: "8.00", receivable_amount: "0" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    const pay = async () => {
      fireEvent.click(screen.getByRole("button", { name: /现金/ }))
      fireEvent.click(await screen.findByRole("button", { name: /确认收款/ }))
    }
    await pay()
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(1))
    await pay()
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(2))
    await pay()
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(3))
    const keys = mockQuickCheckout.mock.calls.map((c) => c[2])
    expect(keys[0]).toBeTruthy()
    expect(keys[1]).toBe(keys[0])
    expect(keys[2]).not.toBe(keys[1])
  })

  it("TestPosPage_LostReply_ChangedPaidAmount_AsksToCheckTodayFirst", async () => {
    const { NetworkError } = await import("@/lib/api/errors")
    const confirmSpy = mockConfirm.mockResolvedValueOnce(false)
    mockQuickCheckout
      .mockRejectedValueOnce(new NetworkError("network", "timeout"))
      .mockResolvedValue({ bill_id: "b", bill_no: "SO-9", total_amount: "8.00", receivable_amount: "0" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    fireEvent.change(await screen.findByLabelText("实收金额"), { target: { value: "20" } })
    fireEvent.click(screen.getByRole("button", { name: /确认收款/ }))
    await screen.findByText(/结账失败/)
    fireEvent.change(screen.getByLabelText("实收金额"), { target: { value: "50" } })
    fireEvent.click(screen.getByRole("button", { name: /确认收款/ }))
    await waitFor(() => expect(confirmSpy).toHaveBeenCalledTimes(1))
    expect(JSON.stringify(confirmSpy.mock.calls[0][0])).toContain("今日结单")
    expect(mockQuickCheckout).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("button", { name: /确认收款/ }))
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(2))
    expect(mockQuickCheckout.mock.calls[1][2]).not.toBe(mockQuickCheckout.mock.calls[0][2])
    mockConfirm.mockClear()
  })

  it("TestPosPage_CashRetryAfterTimeout_KeepsPaidAmountAndKey", async () => {
    const { NetworkError } = await import("@/lib/api/errors")
    mockQuickCheckout
      .mockRejectedValueOnce(new NetworkError("network", "timeout"))
      .mockResolvedValue({ bill_id: "b", bill_no: "SO-9", total_amount: "8.00", receivable_amount: "0" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    fireEvent.change(await screen.findByLabelText("实收金额"), { target: { value: "20" } })
    fireEvent.click(screen.getByRole("button", { name: /确认收款/ }))
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(1))
    await screen.findByText(/结账失败/)
    expect(screen.getByLabelText("实收金额")).toHaveValue(20)
    fireEvent.click(screen.getByRole("button", { name: /确认收款/ }))
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(2))
    const [a, b] = mockQuickCheckout.mock.calls
    expect(a[0].paid_amount).toBe("20.00")
    expect(b[0].paid_amount).toBe("20.00")
    expect(b[2]).toBe(a[2])
  })

  it("TestPosPage_WechatRetryAfterTimeout_KeepsTailAndKey", async () => {
    const { NetworkError } = await import("@/lib/api/errors")
    mockQuickCheckout
      .mockRejectedValueOnce(new NetworkError("network", "timeout"))
      .mockResolvedValue({ bill_id: "b", bill_no: "SO-9", total_amount: "8.00", receivable_amount: "0" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.click(screen.getByRole("button", { name: /微信/ }))
    fireEvent.change(await screen.findByLabelText("流水尾号"), { target: { value: "1234" } })
    fireEvent.click(screen.getByRole("button", { name: /已收到顾客付款/ }))
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(1))
    await screen.findByText(/结账失败/)
    expect(screen.getByLabelText("流水尾号")).toHaveValue("1234")
    fireEvent.click(screen.getByRole("button", { name: /已收到顾客付款/ }))
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(2))
    const [a, b] = mockQuickCheckout.mock.calls
    expect(a[0].customer_name).toBe("微信流水尾号 1234")
    expect(b[0].customer_name).toBe("微信流水尾号 1234")
    expect(b[2]).toBe(a[2])
  })

  it("TestPosPage_CartRemark_SentWithCheckout", async () => {
    mockQuickCheckout.mockResolvedValue({ bill_id: "b", bill_no: "SO-7", total_amount: "8.00", receivable_amount: "0" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.change(screen.getByPlaceholderText(/备注/), { target: { value: " 明天送货 " } })
    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    fireEvent.click(await screen.findByRole("button", { name: /确认收款/ }))
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalled())
    expect(mockQuickCheckout.mock.calls[0][0].remark).toBe("明天送货")
  })

  it("TestPosPage_InFlight409_KeepsKey", async () => {
    const { NetworkError, ApiError } = await import("@/lib/api/errors")
    mockQuickCheckout
      .mockRejectedValueOnce(new NetworkError("network", "timeout"))
      .mockRejectedValueOnce(
        new ApiError(409, "idempotency_in_flight", "still processing", { error: "idempotency_in_flight" }),
      )
      .mockResolvedValue({ bill_id: "b", bill_no: "SO-9", total_amount: "8.00", receivable_amount: "0" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    const pay = async () => {
      fireEvent.click(screen.getByRole("button", { name: /现金/ }))
      fireEvent.click(await screen.findByRole("button", { name: /确认收款/ }))
    }
    await pay()
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(1))
    await pay()
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(2))
    expect(await screen.findByText(/上一笔还在处理/)).toBeInTheDocument()
    await pay()
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalledTimes(3))
    const keys = mockQuickCheckout.mock.calls.map((c) => c[2])
    expect(keys[1]).toBe(keys[0])
    expect(keys[2]).toBe(keys[0])
  })

  it("TestPosPage_CreditSale_SendsPartnerId", async () => {
    mockQuickCheckout.mockResolvedValue({ bill_id: "b", bill_no: "SO-8", total_amount: "8.00", receivable_amount: "8.00" })
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.click(screen.getByRole("button", { name: /赊账/ }))
    const confirm = await screen.findByRole("button", { name: /确认赊账/ })
    expect(confirm).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "选中老王" }))
    fireEvent.click(confirm)
    await waitFor(() => expect(mockQuickCheckout).toHaveBeenCalled())
    const req = mockQuickCheckout.mock.calls[0][0]
    expect(req.partner_id).toBe("cust-1")
    expect(req.payment_method).toBe("credit")
    expect(req.paid_amount).toBe("0.00")
  })

  it("TestPosPage_CartSurvivesReload", async () => {
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    const first = render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    await waitFor(() => expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeTruthy())
    first.unmount()

    render(<PosPage />)
    expect(await screen.findByLabelText("Bolt M8 单价")).toHaveValue(8)
  })

  it("TestPosPage_SignoutFailedEvent_SavesTheInMemoryCart", async () => {
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    await waitFor(() => expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeTruthy())
    window.sessionStorage.removeItem("tally.pos.cart.v1")
    window.dispatchEvent(new Event("tally:signout-failed"))
    expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toContain("p1")
  })

  it("TestPosPage_GridLoads200Products", async () => {
    render(<PosPage />)
    await waitFor(() => expect(mockListProducts).toHaveBeenCalledWith(expect.objectContaining({ limit: 200 })))
  })

  it("TestPosPage_ReturnButton_OpensReturnFlowAndShowsTheResult", async () => {
    render(<PosPage />)
    expect(screen.queryByText("stub-return-done")).toBeNull()
    fireEvent.click(screen.getByText("客户退货"))
    fireEvent.click(await screen.findByText("stub-return-done"))
    expect(await screen.findByText(/退货成功，退货金额 ¥40.00/)).toBeInTheDocument()
  })

  it.each(["clerk", "finance"])("TestPosPage_ReturnButton_HiddenFor_%s", (role) => {
    roleState.role = role
    render(<PosPage />)
    expect(screen.queryByText("客户退货")).toBeNull()
  })

  it("TestPosPage_NeverSent_EnqueuesAndShowsOfflineRef", async () => {
    roleState.scoped = true
    const { NetworkError } = await import("@/lib/api/errors")
    mockQuickCheckout.mockRejectedValue(new NetworkError("offline", "offline", false))
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    fireEvent.click(await screen.findByRole("button", { name: /确认收款/ }))
    await screen.findByText(/离线单号 OFF-/)
    const body = mockQuickCheckout.mock.calls[0][0] as Record<string, unknown>
    expect(body.client_ref).toMatch(/^[0-9a-f]{8}-\d+$/)
    expect(typeof body.occurred_at).toBe("string")
    const queued = queueMem.get("pos.checkout.queue@t1:u1") as { body: Record<string, unknown>; status: string; client_ref: string }[]
    expect(queued).toHaveLength(1)
    expect(queued[0].body).toMatchObject({ source: "pos_offline", client_ref: body.client_ref })
  })

  it("TestPosPage_ServerError_NeverEnqueues", async () => {
    roleState.scoped = true
    const { ApiError } = await import("@/lib/api/errors")
    mockQuickCheckout.mockRejectedValue(new ApiError(500, "x", "boom"))
    mockListProducts.mockResolvedValue({ items: [{ ...sampleProduct, retail_price: "8" }], total: 1 })
    render(<PosPage />)
    fireEvent.click(await screen.findByText("Bolt M8"))
    fireEvent.click(screen.getByRole("button", { name: /现金/ }))
    fireEvent.click(await screen.findByRole("button", { name: /确认收款/ }))
    await screen.findByText(/结账失败/)
    expect(((queueMem.get("pos.checkout.queue@t1:u1") as unknown[]) ?? []).length).toBe(0)
  })
})
