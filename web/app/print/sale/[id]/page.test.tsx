import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "sale-1" }),
  useSearchParams: () => new URLSearchParams(),
}))
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
vi.mock("@/lib/api/customers", () => ({
  getCustomer: vi.fn().mockResolvedValue({ name: "老张", contact_name: "张工", mobile: "13900001111", address: "城北苗木基地 3 号地块" }),
}))
vi.mock("@/lib/api/suppliers", () => ({ getSupplier: vi.fn() }))
vi.mock("@/lib/api/warehouses", () => ({ getWarehouse: vi.fn() }))
vi.mock("@/lib/api/products", () => ({ getProduct: vi.fn().mockResolvedValue({ id: "p-1", name: "雪松", spec: "胸径3cm 冠幅1.2m" }) }))
vi.mock("@/lib/api/print-settings", () => ({
  EMPTY_PRINT_SETTINGS: { shop_name: "", phone: "", address: "", receipt_footer: "" },
  getPrintSettings: vi.fn().mockResolvedValue({ shop_name: "老王苗圃", phone: "138", address: "", receipt_footer: "" }),
}))
vi.mock("@/lib/api/sale", () => ({ getSaleBill: vi.fn() }))

import { getSaleBill } from "@/lib/api/sale"
import { ApiError } from "@/lib/api/errors"
import SalePrintPage from "./page"

const bill = (subType: string) =>
  ({
    head: {
      id: "sale-1",
      bill_no: "SO-1",
      sub_type: subType,
      partner_id: "c-1",
      bill_date: "2026-01-01T00:00:00Z",
      created_at: "2026-01-01T00:00:00Z",
      total_amount: "105.00",
      paid_amount: "5.00",
      receivable_amount: "100.00",
    },
    items: [{ id: "i1", product_id: "p-1", unit_name: "棵", qty: "3", unit_price: "35", line_amount: "105" }],
    payments: [],
  }) as never

describe("SalePrintPage", () => {
  beforeEach(() => vi.clearAllMocks())

  it("prints a delivery note with capital amount and signature line", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(bill("销售"))
    render(<SalePrintPage />)
    expect(await screen.findByText("送货单", { selector: "h1" })).toBeInTheDocument()
    expect(screen.getByText("壹佰零伍元整")).toBeInTheDocument()
    expect(await screen.findByText("雪松")).toBeInTheDocument()
    expect(screen.getByText(/收货人签字/)).toBeInTheDocument()
    expect(await screen.findByText("老王苗圃")).toBeInTheDocument()
  })

  it("marks a return bill as 退货", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(bill("销售退货"))
    render(<SalePrintPage />)
    expect(await screen.findByText("退货单", { selector: "h1" })).toBeInTheDocument()
    expect(screen.getByText("退货", { selector: "span" })).toBeInTheDocument()
  })
  it("stamps a draft as 未审核 and never prints a negative amount owed", async () => {
    const b = bill("销售") as { head: Record<string, unknown> }
    b.head.status = 0
    b.head.receivable_amount = "-10.00"
    vi.mocked(getSaleBill).mockResolvedValue(b as never)
    render(<SalePrintPage />)
    expect(await screen.findByText("未审核", { selector: "span" })).toBeInTheDocument()
    expect(screen.getByText("未收款：¥0.00")).toBeInTheDocument()
  })

  it("states the returned amount after a partial return", async () => {
    const b = bill("销售") as { returned_amount?: string }
    b.returned_amount = "30.00"
    vi.mocked(getSaleBill).mockResolvedValue(b as never)
    render(<SalePrintPage />)
    expect(await screen.findByText("已退货：¥30.00")).toBeInTheDocument()
  })

  it("does not print system import remarks in English", async () => {
    const b = bill("销售") as { head: Record<string, unknown> }
    b.head.remark = "import:shopify:1001"
    vi.mocked(getSaleBill).mockResolvedValue(b as never)
    render(<SalePrintPage />)
    expect(await screen.findByText("备注：网店订单 1001")).toBeInTheDocument()
  })

  it("shows Chinese text when the bill is not found", async () => {
    vi.mocked(getSaleBill).mockRejectedValue(new ApiError(404, "not_found", "sale bill not found"))
    render(<SalePrintPage />)
    expect(await screen.findByText(/找不到这张单据/)).toBeInTheDocument()
    expect(screen.queryByText(/not found/)).toBeNull()
  })

  it("shows the spec column and the consignee contact and address", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(bill("销售"))
    render(<SalePrintPage />)
    expect(await screen.findByText("规格", { selector: "th" })).toBeInTheDocument()
    expect(await screen.findByText("胸径3cm 冠幅1.2m")).toBeInTheDocument()
    expect(await screen.findByText("收货地址：城北苗木基地 3 号地块")).toBeInTheDocument()
    expect(screen.getByText("联系人：张工")).toBeInTheDocument()
    expect(screen.getByText("电话：13900001111")).toBeInTheDocument()
  })

  it("says the bill is not found for a purchase id instead of printing a delivery note", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(bill("采购"))
    render(<SalePrintPage />)
    expect(await screen.findByText(/找不到这张单据/)).toBeInTheDocument()
    expect(screen.queryByText("送货单", { selector: "h1" })).toBeNull()
  })
})
