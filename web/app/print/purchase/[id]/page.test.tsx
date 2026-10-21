import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }))
vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "bill-1" }),
  useRouter: () => ({ replace }),
  useSearchParams: () => new URLSearchParams(),
}))
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
vi.mock("@/lib/api/customers", () => ({ getCustomer: vi.fn() }))
vi.mock("@/lib/api/suppliers", () => ({ getSupplier: vi.fn().mockResolvedValue({ name: "绿源苗圃", contact: "赵师傅", phone: "13800000000", address: "城东苗圃路 8 号" }) }))
vi.mock("@/lib/api/warehouses", () => ({ getWarehouse: vi.fn().mockResolvedValue({ name: "一号仓" }) }))
vi.mock("@/lib/api/products", () => ({ getProduct: vi.fn().mockResolvedValue({ id: "p-1", name: "雪松" }) }))
vi.mock("@/lib/api/print-settings", () => ({
  EMPTY_PRINT_SETTINGS: { shop_name: "", phone: "", address: "", receipt_footer: "" },
  getPrintSettings: vi.fn().mockResolvedValue({ shop_name: "老王苗圃", phone: "", address: "", receipt_footer: "" }),
}))
vi.mock("@/lib/api/purchase", () => ({ getPurchaseBill: vi.fn() }))

import { getProduct } from "@/lib/api/products"
import { getPurchaseBill } from "@/lib/api/purchase"
import PurchasePrintPage from "./page"

const bill = (subType: string) =>
  ({
    head: {
      id: "bill-1",
      bill_no: "PO-1",
      sub_type: subType,
      partner_id: "s-1",
      warehouse_id: "w-1",
      bill_date: "2026-01-01T00:00:00Z",
      total_amount: "105.00",
    },
    items: [{ id: "i1", product_id: "p-1", unit_name: "棵", qty: "3", unit_price: "35", line_amount: "105" }],
  }) as never

describe("PurchasePrintPage", () => {
  beforeEach(() => vi.clearAllMocks())

  it("prints a purchase receipt", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(bill("采购"))
    render(<PurchasePrintPage />)
    expect(await screen.findByText("采购入库单", { selector: "h1" })).toBeInTheDocument()
  })

  it("shows the supplier contact, phone and address in the header", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(bill("采购"))
    render(<PurchasePrintPage />)
    expect(await screen.findByText("联系人：赵师傅")).toBeInTheDocument()
    expect(screen.getByText("电话：13800000000")).toBeInTheDocument()
    expect(screen.getByText("地址：城东苗圃路 8 号")).toBeInTheDocument()
  })

  it("warns in the toolbar when product details did not load", async () => {
    vi.mocked(getProduct).mockRejectedValueOnce(new Error("boom"))
    vi.mocked(getPurchaseBill).mockResolvedValue(bill("采购"))
    render(<PurchasePrintPage />)
    expect(await screen.findByText("有商品信息没加载出来，请刷新后再打印")).toBeInTheDocument()
  })

  it("marks a purchase return as 退货", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(bill("采购退货"))
    render(<PurchasePrintPage />)
    expect(await screen.findByText("采购退货单", { selector: "h1" })).toBeInTheDocument()
    expect(screen.getByText("退货", { selector: "span" })).toBeInTheDocument()
  })

  it("sends a sale return to the sale sheet", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(bill("销售退货"))
    render(<PurchasePrintPage />)
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/print/sale/bill-1"))
    expect(screen.queryByText("采购入库单")).toBeNull()
  })

  it("treats any other bill type as not found", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(bill("销售"))
    render(<PurchasePrintPage />)
    expect(await screen.findByText(/找不到这张单据/)).toBeInTheDocument()
    expect(replace).not.toHaveBeenCalled()
  })
})
