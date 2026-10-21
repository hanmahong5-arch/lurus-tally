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
vi.mock("@/lib/api/products", () => ({ getProduct: vi.fn().mockResolvedValue({ id: "p-1", name: "螺丝刀" }) }))
vi.mock("@/lib/api/print-settings", () => ({
  EMPTY_PRINT_SETTINGS: { shop_name: "", phone: "", address: "", receipt_footer: "" },
  getPrintSettings: vi.fn().mockResolvedValue({ shop_name: "老王五金", phone: "", address: "", receipt_footer: "" }),
}))
vi.mock("@/lib/api/sale", () => ({ getSaleBill: vi.fn() }))

import { getSaleBill } from "@/lib/api/sale"
import PosPrintPage from "./page"

const bill = () =>
  ({
    head: {
      id: "sale-1",
      bill_no: "SO-1",
      sub_type: "销售",
      created_at: "2026-01-01T10:00:00Z",
      total_amount: "12.30",
      paid_amount: "12.30",
      receivable_amount: "0",
    },
    items: [{ id: "i1", product_id: "p-1", unit_name: "把", qty: "1", unit_price: "12.30", line_amount: "12.30" }],
    payments: [{ id: "pay1", bill_id: "sale-1", amount: "12.30", pay_type: "cash", pay_date: "2026-01-01" }],
  }) as never

describe("PosPrintPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
  })

  it("prints pay method, cash tendered and change from the checkout screen", async () => {
    window.localStorage.setItem("tally-pos-tender:sale-1", JSON.stringify({ received: "100.00", change: "87.70" }))
    vi.mocked(getSaleBill).mockResolvedValue(bill())
    render(<PosPrintPage />)
    expect(await screen.findByText("付款方式")).toBeInTheDocument()
    expect(screen.getByText("现金")).toBeInTheDocument()
    expect(await screen.findByText("¥100.00")).toBeInTheDocument()
    expect(screen.getByText("找零")).toBeInTheDocument()
    expect(screen.getByText("¥87.70")).toBeInTheDocument()
  })

  it("takes cash tendered and change from the payment when this device never saw them", async () => {
    const b = bill() as unknown as { payments: Record<string, string>[] }
    b.payments[0].remark = "实收 100.00，找零 87.70"
    vi.mocked(getSaleBill).mockResolvedValue(b as never)
    render(<PosPrintPage />)
    expect(await screen.findByText("¥100.00")).toBeInTheDocument()
    expect(screen.getByText("找零")).toBeInTheDocument()
    expect(screen.getByText("¥87.70")).toBeInTheDocument()
  })

  it("reprints without change when this device never saw the tender", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(bill())
    render(<PosPrintPage />)
    expect(await screen.findByText("实收")).toBeInTheDocument()
    expect(screen.queryByText("找零")).toBeNull()
  })

  it("refuses a purchase bill id", async () => {
    const b = bill() as unknown as { head: { sub_type: string } }
    b.head.sub_type = "采购"
    vi.mocked(getSaleBill).mockResolvedValue(b as never)
    render(<PosPrintPage />)
    expect(await screen.findByText(/找不到这张单据/)).toBeInTheDocument()
  })

  it("shows 赊账 as pay method for a fully credited sale", async () => {
    const b = bill() as unknown as { head: Record<string, string>; payments: unknown[] }
    b.head.paid_amount = "0"
    b.head.receivable_amount = "12.30"
    b.payments = []
    vi.mocked(getSaleBill).mockResolvedValue(b as never)
    render(<PosPrintPage />)
    expect(await screen.findByText("赊账")).toBeInTheDocument()
  })
})
