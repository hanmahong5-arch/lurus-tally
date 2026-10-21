/**
 * Unit tests for purchases/new/page.tsx draft integration.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import * as idbStorage from "@/lib/draft/idb-storage"
import * as purchaseApi from "@/lib/api/purchase"

const { push, searchParams } = vi.hoisted(() => ({ push: vi.fn(), searchParams: { edit: null as string | null, prefill: null as string | null } }))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, back: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => ({
    get: (k: string) => (k === "edit" ? searchParams.edit : k === "prefill_product_id" ? searchParams.prefill : null),
  }),
}))
vi.mock("@/lib/api/products", () => ({ getProduct: vi.fn().mockResolvedValue({ id: "p-1", name: "雪松" }) }))
vi.mock("@/lib/api/product-units", () => ({
  listProductUnits: vi.fn().mockResolvedValue([{ id: "u-base", name: "株", is_base: true }]),
}))
vi.mock("@/lib/api/suppliers", () => ({ getSupplier: vi.fn().mockResolvedValue({ id: "sup-1", name: "绿源苗圃" }) }))

const { roleState } = vi.hoisted(() => ({ roleState: { known: false, perms: [] as string[] } }))
vi.mock("@/hooks/use-role", () => ({
  useRole: () => ({ known: roleState.known, can: (p: string) => roleState.perms.includes(p) }),
}))
const { toastSuccess } = vi.hoisted(() => ({ toastSuccess: vi.fn() }))
vi.mock("sonner", () => ({ toast: { success: toastSuccess, error: vi.fn() } }))

vi.mock("@/lib/draft/idb-storage")
vi.mock("@/lib/api/purchase", () => ({
  createPurchaseBill: vi.fn(),
  getPurchaseBill: vi.fn(),
  updatePurchaseBill: vi.fn(),
}))

// Supplier picker stub: one button that "picks" a supplier.
vi.mock("@/components/entity-pickers", () => ({
  SupplierPicker: ({ onSelect }: { onSelect: (o: { id: string; name: string }) => void }) => (
    <button type="button" onClick={() => onSelect({ id: "sup-1", name: "绿源苗圃" })}>
      pick-supplier
    </button>
  ),
}))

// Warehouse select stub: one button that "picks" a warehouse.
vi.mock("@/components/warehouse-select", () => ({
  WarehouseSelect: ({ onChange }: { onChange: (id: string) => void }) => (
    <button type="button" onClick={() => onChange("wh-1")}>
      pick-warehouse
    </button>
  ),
}))

vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: "cross_border" }),
  ProfileGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

// Stub out BillLineEditor (does network calls).
vi.mock("@/components/bill-line-editor", () => ({
  BillLineEditor: ({ onChange, items }: { onChange: (items: unknown[]) => void; items: { qty: string; product_name?: string; unit_price?: string }[] }) => (
    <button
      data-lines={items.map((it) => `${it.product_name}x${it.qty}`).join(",")}
      data-prices={items.map((it) => (it as { unit_price?: string }).unit_price).join(",")}
      type="button"
      data-testid="bill-line-editor"
      onClick={() => onChange([{ product_id: "p-1", product_name: "雪松", qty: "2", unit_price: "10", line_no: 1 }])}
    >
      add-line
    </button>
  ),
}))

// Quantity-only lines for people who cannot see prices: stubbed the same way.
vi.mock("@/components/purchase-draft-lines", () => ({
  PurchaseDraftLines: ({ onChange }: { onChange: (items: unknown[]) => void }) => (
    <button
      type="button"
      data-testid="draft-lines"
      onClick={() => onChange([{ product_id: "p-1", product_name: "雪松", qty: "2", unit_price: "0", line_no: 1 }])}
    >
      add-line
    </button>
  ),
}))

// Stub cross-border components.
vi.mock("@/components/cross-border/currency-selector", () => ({
  CurrencySelector: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <select value={value} onChange={(e) => onChange(e.target.value)} data-testid="currency-selector" />
  ),
}))
vi.mock("@/components/cross-border/rate-input", () => ({
  RateInput: () => <input data-testid="rate-input" />,
}))

import NewPurchasePage from "./page"

describe("purchases/new/page.tsx — draft integration", () => {
  beforeEach(() => {
    push.mockClear()
    toastSuccess.mockClear()
    roleState.known = false
    roleState.perms = []
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("TestPurchaseNewPage_DraftRestored_ShowsToast", async () => {
    const savedAt = new Date().toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { billDate: "2025-01-01", remark: "草稿采购单" },
      savedAt,
    })

    render(<NewPurchasePage />)

    await waitFor(() => {
      expect(screen.getByText(/已恢复/)).toBeTruthy()
    })
  })

  it("北京时间早上 7 点打开，单据日期默认是当天", async () => {
    vi.useFakeTimers({ toFake: ["Date"] })
    vi.setSystemTime(new Date("2026-10-09T23:00:00Z"))
    try {
      const { container } = render(<NewPurchasePage />)
      const input = container.querySelector('input[type="date"]') as HTMLInputElement
      expect(input.value).toBe("2026-10-10")
    } finally {
      vi.useRealTimers()
    }
  })

  it("TestPurchaseNewPage_NoSupplier_BlocksSubmit", async () => {
    render(<NewPurchasePage />)
    fireEvent.click(screen.getByText("add-line"))
    fireEvent.click(screen.getByText("保存草稿"))
    expect(await screen.findByText("请选择供应商")).toBeTruthy()
    expect(purchaseApi.createPurchaseBill).not.toHaveBeenCalled()
  })

  it("TestPurchaseNewPage_WithSupplier_SendsPartnerId", async () => {
    vi.mocked(purchaseApi.createPurchaseBill).mockResolvedValue({ bill_id: "b-1" } as never)
    render(<NewPurchasePage />)
    fireEvent.click(screen.getByText("pick-supplier"))
    fireEvent.click(screen.getByText("pick-warehouse"))
    fireEvent.click(screen.getByText("add-line"))
    fireEvent.click(screen.getByText("保存草稿"))
    await waitFor(() => expect(purchaseApi.createPurchaseBill).toHaveBeenCalled())
    const body = vi.mocked(purchaseApi.createPurchaseBill).mock.calls[0][0]
    expect(body.partner_id).toBe("sup-1")
    expect(body.warehouse_id).toBe("wh-1")
    expect(body.items[0]).not.toHaveProperty("product_name")
    expect(vi.mocked(purchaseApi.createPurchaseBill).mock.calls[0][2]).toEqual(expect.any(String))
  })
})

describe("purchases/new/page.tsx — edit a draft", () => {
  const draftBill = {
    head: {
      id: "po-1",
      status: 0,
      partner_id: "sup-1",
      warehouse_id: "wh-1",
      bill_date: "2026-10-01T03:00:00Z",
      shipping_fee: "5",
      tax_amount: "0",
      remark: "旧备注",
    },
    items: [{ id: "i-1", product_id: "p-1", line_no: 1, qty: "10", unit_price: "8", line_amount: "80" }],
  }

  beforeEach(() => {
    searchParams.edit = "po-1"
    push.mockClear()
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
    vi.mocked(purchaseApi.getPurchaseBill).mockReset()
    vi.mocked(purchaseApi.updatePurchaseBill).mockReset()
    vi.mocked(purchaseApi.createPurchaseBill).mockReset()
  })

  afterEach(() => {
    searchParams.edit = null
  })

  it("prefills the form from the draft and saves with a PUT, not a new bill", async () => {
    vi.mocked(purchaseApi.getPurchaseBill).mockResolvedValue(draftBill as never)
    vi.mocked(purchaseApi.updatePurchaseBill).mockResolvedValue({} as never)
    render(<NewPurchasePage />)

    expect(await screen.findByText("修改采购单")).toBeTruthy()
    await waitFor(() => expect(screen.getByTestId("bill-line-editor").getAttribute("data-lines")).toBe("雪松x10"))
    expect(screen.getByDisplayValue("旧备注")).toBeTruthy()

    fireEvent.click(screen.getByText("保存修改"))
    await waitFor(() => expect(purchaseApi.updatePurchaseBill).toHaveBeenCalled())
    const [id, body] = vi.mocked(purchaseApi.updatePurchaseBill).mock.calls[0]
    expect(id).toBe("po-1")
    expect(body.partner_id).toBe("sup-1")
    expect(body.warehouse_id).toBe("wh-1")
    expect(body.shipping_fee).toBe("5")
    expect(body.items[0]).toMatchObject({ product_id: "p-1", qty: "10", unit_price: "8" })
    // the date was not touched, so the draft keeps its own
    expect(body.bill_date).toBeUndefined()
    expect(purchaseApi.createPurchaseBill).not.toHaveBeenCalled()
    await waitFor(() => expect(push).toHaveBeenCalledWith("/purchases/po-1"))
  })

  it("refuses to edit a bill that is no longer a draft", async () => {
    vi.mocked(purchaseApi.getPurchaseBill).mockResolvedValue({ ...draftBill, head: { ...draftBill.head, status: 2 } } as never)
    render(<NewPurchasePage />)
    expect(await screen.findByText("只有草稿采购单可以修改，这张单已经审核或作废了")).toBeTruthy()
    expect(screen.getByText("保存修改").closest("button")?.disabled).toBe(true)
  })

  it("does not show a restored-draft toast in edit mode", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue({ value: { remark: "别的草稿" }, savedAt: new Date().toISOString() })
    vi.mocked(purchaseApi.getPurchaseBill).mockResolvedValue(draftBill as never)
    render(<NewPurchasePage />)
    await waitFor(() => expect(screen.getByTestId("bill-line-editor").getAttribute("data-lines")).toBe("雪松x10"))
    expect(screen.queryByText(/已恢复/)).toBeNull()
    expect(screen.getByDisplayValue("旧备注")).toBeTruthy()
  })
})

describe("purchases/new/page.tsx — 去补货 prefill", () => {
  beforeEach(() => {
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
    searchParams.prefill = "p-1"
  })
  afterEach(() => {
    searchParams.prefill = null
  })

  it("starts with one line holding the product to restock", async () => {
    render(<NewPurchasePage />)
    await waitFor(() => expect(screen.getByTestId("bill-line-editor").getAttribute("data-lines")).toBe("雪松x1"))
  })

  it("carries the SKU price over when it is quoted in the base unit", async () => {
    const { getProduct } = await import("@/lib/api/products")
    vi.mocked(getProduct).mockResolvedValueOnce({ id: "p-1", name: "雪松", purchase_price: "12.50", default_unit_id: "u-base" } as never)
    render(<NewPurchasePage />)
    await waitFor(() => expect(screen.getByTestId("bill-line-editor").getAttribute("data-prices")).toBe("12.50"))
  })

  it("leaves the price empty when the SKU price is quoted in another unit", async () => {
    const { getProduct } = await import("@/lib/api/products")
    vi.mocked(getProduct).mockResolvedValueOnce({ id: "p-1", name: "雪松", purchase_price: "300", default_unit_id: "u-box" } as never)
    render(<NewPurchasePage />)
    await waitFor(() => expect(screen.getByTestId("bill-line-editor").getAttribute("data-lines")).toBe("雪松x1"))
    expect(screen.getByTestId("bill-line-editor").getAttribute("data-prices")).toBe("0")
  })

  it("does not claim a draft was restored when the prefill wins", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { remark: "旧草稿", items: [{ product_id: "p-9", product_name: "旧商品", qty: "3", unit_price: "1", line_no: 1 }] },
      savedAt: new Date().toISOString(),
    })
    render(<NewPurchasePage />)
    await waitFor(() => expect(screen.getByTestId("bill-line-editor").getAttribute("data-lines")).toBe("雪松x1"))
    await new Promise((r) => setTimeout(r, 30))
    expect(screen.queryByText(/已恢复/)).toBeNull()
  })

  it("wins over an older unsaved draft", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { remark: "旧草稿", items: [{ product_id: "p-9", product_name: "旧商品", qty: "3", unit_price: "1", line_no: 1 }] },
      savedAt: new Date().toISOString(),
    })
    render(<NewPurchasePage />)
    await waitFor(() => expect(screen.getByTestId("bill-line-editor").getAttribute("data-lines")).toBe("雪松x1"))
    await new Promise((r) => setTimeout(r, 30))
    expect(screen.getByTestId("bill-line-editor").getAttribute("data-lines")).toBe("雪松x1")
  })

  async function saveNewDraft() {
    push.mockClear()
    toastSuccess.mockClear()
    vi.mocked(purchaseApi.createPurchaseBill).mockResolvedValue({ bill_id: "b-9" } as never)
    render(<NewPurchasePage />)
    fireEvent.click(screen.getByText("pick-supplier"))
    fireEvent.click(screen.getByText("pick-warehouse"))
    fireEvent.click(screen.getByText("add-line"))
    fireEvent.click(screen.getByText("保存草稿"))
    await waitFor(() => expect(purchaseApi.createPurchaseBill).toHaveBeenCalled())
  }

  it("店员保存采购草稿后回到采购列表并提示等审核", async () => {
    roleState.known = true
    roleState.perms = ["bill.draft"]
    await saveNewDraft()
    await waitFor(() => expect(push).toHaveBeenCalledWith("/purchases"))
    expect(toastSuccess).toHaveBeenCalledWith("采购草稿已保存，等老板或店长审核")
  })

  it("店员的采购单只有商品和数量：不画单价，不要求单价，也不发送价格", async () => {
    roleState.known = true
    roleState.perms = ["bill.draft"]
    render(<NewPurchasePage />)
    expect(screen.queryByTestId("bill-line-editor")).toBeNull()
    expect(screen.queryByTestId("currency-selector")).toBeNull()
    fireEvent.click(screen.getByText("pick-supplier"))
    fireEvent.click(screen.getByTestId("draft-lines"))
    fireEvent.click(screen.getByText("保存草稿"))
    await waitFor(() => expect(purchaseApi.createPurchaseBill).toHaveBeenCalled())
    const body = vi.mocked(purchaseApi.createPurchaseBill).mock.calls[0][0] as { items: { unit_price: string }[] }
    expect(body.items.map((it) => it.unit_price)).toEqual(["0"])
  })

  it("店长保存采购草稿后进入单据详情", async () => {
    roleState.known = true
    roleState.perms = ["bill.draft", "cost.view"]
    await saveNewDraft()
    await waitFor(() => expect(push).toHaveBeenCalledWith("/purchases/b-9"))
    expect(toastSuccess).not.toHaveBeenCalledWith("采购草稿已保存，等老板或店长审核")
  })

  it("财务没有开单权限：显示说明页，不画表单", async () => {
    roleState.known = true
    roleState.perms = ["cost.view", "purchase.view"]
    render(<NewPurchasePage />)
    expect(await screen.findByText("你没有开单的权限")).toBeInTheDocument()
    expect(screen.queryByText("保存草稿")).toBeNull()
  })
})
