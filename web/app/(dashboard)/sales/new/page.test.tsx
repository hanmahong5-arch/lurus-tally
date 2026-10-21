/**
 * Unit tests for sales/new/page.tsx draft integration.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import * as idbStorage from "@/lib/draft/idb-storage"
import * as saleApi from "@/lib/api/sale"
import { toast } from "sonner"
import { ApiError } from "@/lib/api/errors"

const { push, searchParams } = vi.hoisted(() => ({ push: vi.fn(), searchParams: { edit: null as string | null } }))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, back: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => ({ get: (k: string) => (k === "edit" ? searchParams.edit : null) }),
}))
vi.mock("@/lib/api/products", () => ({ getProduct: vi.fn().mockResolvedValue({ id: "p-1", name: "雪松" }) }))
vi.mock("@/lib/api/customers", () => ({ getCustomer: vi.fn().mockResolvedValue({ id: "cus-1", name: "老张" }) }))

vi.mock("@/lib/draft/idb-storage")
vi.mock("sonner", () => ({ toast: { info: vi.fn(), error: vi.fn(), success: vi.fn() } }))
vi.mock("@/lib/api/sale", () => ({
  createSaleBill: vi.fn(),
  quickCheckout: vi.fn(),
  getSaleBill: vi.fn(),
  updateSaleBill: vi.fn(),
}))

vi.mock("@/components/entity-pickers", () => ({
  CustomerPicker: ({ onSelect }: { onSelect: (o: { id: string; name: string }) => void }) => (
    <button type="button" onClick={() => onSelect({ id: "cus-1", name: "老张" })}>
      pick-customer
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

const profile = vi.hoisted(() => ({ type: "cross_border" }))
// Shop role of the signed-in person; null means the session has not told us yet.
const roleState = vi.hoisted(() => ({ role: null as string | null }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({
    data: roleState.role ? { user: { role: roleState.role, isOwner: roleState.role === "owner" } } : null,
  }),
}))

vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: profile.type }),
  ProfileGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

vi.mock("@/components/sale-line-editor", () => ({
  SaleLineEditor: ({ onChange, items }: { onChange: (items: unknown[]) => void; items: { qty: string; product_name?: string }[] }) => (
    <>
    <button
      type="button"
      data-testid="cents-one-line"
      onClick={() => onChange([{ product_id: "p-1", product_name: "雪松", qty: "3.33", unit_price: "12.5", line_no: 1 }])}
    >
      cents-one-line
    </button>
    <button
      type="button"
      data-testid="cents-two-lines"
      onClick={() =>
        onChange([
          { product_id: "p-1", product_name: "雪松", qty: "3.33", unit_price: "1.5", line_no: 1 },
          { product_id: "p-2", product_name: "红枫", qty: "3.33", unit_price: "1.5", line_no: 2 },
        ])
      }
    >
      cents-two-lines
    </button>
    <button
      type="button"
      data-lines={items.map((it) => `${it.product_name}x${it.qty}`).join(",")}
      data-testid="sale-line-editor"
      onClick={() => onChange([{ product_id: "p-1", product_name: "雪松", qty: "1", unit_price: "50", line_no: 1 }])}
    >
      add-line
    </button>
    </>
  ),
}))

vi.mock("@/components/cross-border/currency-selector", () => ({
  CurrencySelector: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <select value={value} onChange={(e) => onChange(e.target.value)} data-testid="currency-selector" />
  ),
}))

vi.mock("@/components/cross-border/rate-input", () => ({
  RateInput: () => <input data-testid="rate-input" />,
}))

import NewSalePage from "./page"
import { saveBillCopy } from "@/lib/bill-copy"

describe("sales/new/page.tsx — draft integration", () => {
  beforeEach(() => {
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("TestSaleNewPage_DraftRestored_ShowsToast", async () => {
    const savedAt = new Date().toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { customerName: "测试客户", billDate: "2025-01-01" },
      savedAt,
    })

    render(<NewSalePage />)

    await waitFor(() => {
      expect(screen.getByText(/已恢复/)).toBeTruthy()
    })
  })

  it("TestSaleNewPage_DraftMode_SendsCustomerAsPartner", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(saleApi.createSaleBill).mockResolvedValue({ bill_id: "s-1" } as never)
    render(<NewSalePage />)
    fireEvent.click(screen.getByText("pick-customer"))
    fireEvent.click(screen.getByText("pick-warehouse"))
    fireEvent.click(screen.getByText("add-line"))
    fireEvent.click(screen.getByText("保存草稿"))
    await waitFor(() => expect(saleApi.createSaleBill).toHaveBeenCalled())
    const body = vi.mocked(saleApi.createSaleBill).mock.calls[0][0]
    expect(body.partner_id).toBe("cus-1")
    expect(body.warehouse_id).toBe("wh-1")
  })

  it("TestSaleNewPage_QuickMode_SendsPartnerIdAndWarehouse", async () => {
    vi.mocked(saleApi.quickCheckout).mockResolvedValue({ bill_id: "s-2", change_amount: "5" } as never)
    render(<NewSalePage />)
    fireEvent.click(screen.getByText("切换为快速收银"))
    fireEvent.click(screen.getByText("pick-customer"))
    fireEvent.click(screen.getByText("pick-warehouse"))
    fireEvent.click(screen.getByText("add-line"))
    fireEvent.change(screen.getByLabelText(/实收金额/), { target: { value: "55" } })
    fireEvent.click(screen.getByText("确认收银"))
    await waitFor(() => expect(saleApi.quickCheckout).toHaveBeenCalled())
    const body = vi.mocked(saleApi.quickCheckout).mock.calls[0][0]
    expect(body.partner_id).toBe("cus-1")
    expect(body.customer_name).toBeUndefined()
    // the backend reads the outbound warehouse from the lines, not the header
    expect(body.items.length).toBeGreaterThan(0)
    for (const it of body.items) expect(it.warehouse_id).toBe("wh-1")
    expect(toast.info).toHaveBeenCalledWith("找零 ¥5.00（不计入收款）")
    expect(vi.mocked(saleApi.quickCheckout).mock.calls[0][2]).toEqual(expect.any(String))
  })

  it("TestSaleNewPage_QuickMode_InsufficientStock_NamesProductAndLinksToStock", async () => {
    vi.mocked(saleApi.quickCheckout).mockRejectedValue(
      new ApiError(422, "insufficient_stock", "HTTP 422", {
        error: "insufficient_stock",
        message: "库存不足",
        details: [{ product_id: "p-1", available_qty: "1", requested_qty: "4" }],
      })
    )
    render(<NewSalePage />)
    fireEvent.click(screen.getByText("切换为快速收银"))
    fireEvent.click(screen.getByText("pick-warehouse"))
    fireEvent.click(screen.getByText("cents-one-line"))
    fireEvent.change(screen.getByLabelText(/实收金额/), { target: { value: "55" } })
    fireEvent.click(screen.getByText("确认收银"))
    expect(await screen.findByText("「雪松」库存不足：可用 1，本单需要 4")).toBeTruthy()
    expect(screen.getByText("去盘点或进货").closest("a")?.getAttribute("href")).toBe("/stock")
  })

  it("TestSaleNewPage_DraftSaveFailure_AfterShortage_ClearsStockLink", async () => {
    vi.mocked(saleApi.quickCheckout).mockRejectedValue(
      new ApiError(422, "insufficient_stock", "HTTP 422", {
        error: "insufficient_stock",
        message: "库存不足",
        details: [{ product_id: "p-1", available_qty: "1", requested_qty: "4" }],
      })
    )
    vi.mocked(saleApi.createSaleBill).mockRejectedValue(new ApiError(500, "boom", "保存失败"))
    render(<NewSalePage />)
    fireEvent.click(screen.getByText("切换为快速收银"))
    fireEvent.click(screen.getByText("pick-warehouse"))
    fireEvent.click(screen.getByText("cents-one-line"))
    fireEvent.change(screen.getByLabelText(/实收金额/), { target: { value: "55" } })
    fireEvent.click(screen.getByText("确认收银"))
    await screen.findByText("去盘点或进货")
    fireEvent.click(screen.getByText("切换为草稿模式"))
    fireEvent.click(screen.getByText("保存草稿"))
    await waitFor(() => expect(saleApi.createSaleBill).toHaveBeenCalled())
    await waitFor(() => expect(screen.queryByText("去盘点或进货")).toBeNull())
  })

  it("TestSaleNewPage_Horticulture_RemarkPlaceholderMentionsSite", () => {
    profile.type = "horticulture"
    try {
      render(<NewSalePage />)
      expect(screen.getByPlaceholderText("送到哪个工地、联系人和电话写在这里，会印在送货单上")).toBeTruthy()
    } finally {
      profile.type = "cross_border"
    }
  })

  it("TestSaleNewPage_QuickMode_OneLineToCent_FullButtonFillsPayable", async () => {
    render(<NewSalePage />)
    fireEvent.click(screen.getByText("切换为快速收银"))
    fireEvent.click(screen.getByText("cents-one-line"))
    // 12.5 x 3.33 = 41.625 -> 41.63
    expect(screen.getAllByText(/41\.63/).length).toBeGreaterThan(0)
    fireEvent.click(screen.getByText("全额"))
    expect((screen.getByLabelText(/实收金额/) as HTMLInputElement).value).toBe("41.63")
  })

  it("TestSaleNewPage_QuickMode_TwoLinesEachToCent_PayableIsTen", async () => {
    render(<NewSalePage />)
    fireEvent.click(screen.getByText("切换为快速收银"))
    fireEvent.click(screen.getByText("cents-two-lines"))
    expect(screen.getAllByText(/10\.00/).length).toBeGreaterThan(0)
    expect(screen.queryByText(/9\.99/)).toBeNull()
  })

  it("TestSaleNewPage_CopiedForeignBill_KeepsCurrencyAndRate", async () => {
    sessionStorage.clear()
    saveBillCopy("sale", {
      partner_id: "cus-9",
      partner_name: "老外",
      warehouse_id: "wh-1",
      currency: "USD",
      exchange_rate: "7.2",
      items: [{ product_id: "p-1", product_name: "雪松", qty: "2", unit_price: "10" }],
    })
    vi.mocked(saleApi.createSaleBill).mockResolvedValue({ bill_id: "s-3" } as never)
    render(<NewSalePage />)
    fireEvent.click(await screen.findByText("保存草稿"))
    await waitFor(() => expect(saleApi.createSaleBill).toHaveBeenCalled())
    const body = vi.mocked(saleApi.createSaleBill).mock.calls.at(-1)![0]
    expect(body.partner_id).toBe("cus-9")
    expect(body.currency).toBe("USD")
    expect(body.exchange_rate).toBe("7.2")
    expect(body.items[0].unit_price).toBe("10")
  })
})

describe("sales/new/page.tsx — edit a draft", () => {
  const draftSale = {
    head: {
      id: "so-1",
      status: 0,
      partner_id: "cus-1",
      warehouse_id: "wh-1",
      bill_date: "2026-10-01T03:00:00Z",
      remark: "旧备注",
    },
    items: [{ id: "i-1", product_id: "p-1", line_no: 1, qty: "5", unit_price: "20", line_amount: "100" }],
    payments: [],
  }

  beforeEach(() => {
    searchParams.edit = "so-1"
    push.mockClear()
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
    vi.mocked(saleApi.getSaleBill).mockReset()
    vi.mocked(saleApi.updateSaleBill).mockReset()
    vi.mocked(saleApi.createSaleBill).mockReset()
    vi.mocked(saleApi.quickCheckout).mockReset()
  })

  afterEach(() => {
    searchParams.edit = null
  })

  it("prefills the form from the draft and saves with a PUT, never a new bill or a checkout", async () => {
    vi.mocked(saleApi.getSaleBill).mockResolvedValue(draftSale as never)
    vi.mocked(saleApi.updateSaleBill).mockResolvedValue({} as never)
    render(<NewSalePage />)

    expect(await screen.findByText("修改销售单")).toBeTruthy()
    // edit mode is always the draft form, never the quick-checkout one
    expect(screen.queryByText("切换为快速收银")).toBeNull()
    await waitFor(() => expect(screen.getByTestId("sale-line-editor").getAttribute("data-lines")).toBe("雪松x5"))

    fireEvent.click(screen.getByText("保存修改"))
    await waitFor(() => expect(saleApi.updateSaleBill).toHaveBeenCalled())
    const [id, body] = vi.mocked(saleApi.updateSaleBill).mock.calls[0]
    expect(id).toBe("so-1")
    expect(body.partner_id).toBe("cus-1")
    expect(body.warehouse_id).toBe("wh-1")
    expect(body.remark).toBe("旧备注")
    expect(body.items[0]).toMatchObject({ product_id: "p-1", qty: "5", unit_price: "20" })
    expect(body.bill_date).toBeUndefined()
    expect(saleApi.createSaleBill).not.toHaveBeenCalled()
    expect(saleApi.quickCheckout).not.toHaveBeenCalled()
    await waitFor(() => expect(push).toHaveBeenCalledWith("/sales/so-1"))
  })

  it("sends a new date only when the clerk changed it", async () => {
    vi.mocked(saleApi.getSaleBill).mockResolvedValue(draftSale as never)
    vi.mocked(saleApi.updateSaleBill).mockResolvedValue({} as never)
    render(<NewSalePage />)
    await waitFor(() => expect(screen.getByTestId("sale-line-editor").getAttribute("data-lines")).toBe("雪松x5"))
    fireEvent.change(screen.getByLabelText("单据日期"), { target: { value: "2026-10-05" } })
    fireEvent.click(screen.getByText("保存修改"))
    await waitFor(() => expect(saleApi.updateSaleBill).toHaveBeenCalled())
    expect(vi.mocked(saleApi.updateSaleBill).mock.calls[0][1].bill_date).toMatch(/^2026-10-05/)
  })

  it("refuses to edit a sale that is no longer a draft", async () => {
    vi.mocked(saleApi.getSaleBill).mockResolvedValue({ ...draftSale, head: { ...draftSale.head, status: 2 } } as never)
    render(<NewSalePage />)
    expect(await screen.findByText("只有草稿销售单可以修改，这张单已经审核或作废了")).toBeTruthy()
    expect(screen.getByText("保存修改").closest("button")?.disabled).toBe(true)
  })
})

describe("sales/new/page.tsx — by shop role", () => {
  beforeEach(() => {
    searchParams.edit = null
    profile.type = "retail"
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
  })

  afterEach(() => {
    roleState.role = null
    profile.type = "cross_border"
  })

  it("opens quick checkout for a manager and offers the switch to a draft", async () => {
    roleState.role = "manager"
    render(<NewSalePage />)
    expect(await screen.findByText("确认收银")).toBeTruthy()
    expect(screen.getByText("切换为草稿模式")).toBeTruthy()
  })

  it("keeps a clerk on the draft form: no quick checkout, no switch", async () => {
    roleState.role = "clerk"
    render(<NewSalePage />)
    expect(await screen.findByText("保存草稿")).toBeTruthy()
    expect(screen.queryByText("确认收银")).toBeNull()
    expect(screen.queryByText("切换为快速收银")).toBeNull()
  })

  it("keeps a cashier on quick checkout with no way to draft", async () => {
    roleState.role = "cashier"
    render(<NewSalePage />)
    expect(await screen.findByText("确认收银")).toBeTruthy()
    expect(screen.queryByText("切换为草稿模式")).toBeNull()
  })

  it("tells finance to ask the boss instead of drawing the form", async () => {
    roleState.role = "finance"
    render(<NewSalePage />)
    expect(await screen.findByText("你没有开单的权限")).toBeTruthy()
    expect(screen.queryByText("确认收银")).toBeNull()
    expect(screen.queryByText("保存草稿")).toBeNull()
  })
})
