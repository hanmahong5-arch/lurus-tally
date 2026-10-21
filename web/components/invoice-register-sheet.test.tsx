import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import * as invoiceApi from "@/lib/api/invoices"
import * as saleApi from "@/lib/api/sale"
import { InvoiceRegisterSheet } from "./invoice-register-sheet"

vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("@/components/entity-pickers", () => ({
  CustomerPicker: ({ label, onSelect }: { label: string; onSelect: (o: { id: string; name: string }) => void }) => (
    <button type="button" onClick={() => onSelect({ id: "c-1", name: "老王花店" })}>
      选客户:{label}
    </button>
  ),
  SupplierPicker: () => <div>供应商选择</div>,
}))
vi.mock("@/lib/api/sale", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/sale")>("@/lib/api/sale")
  return { ...actual, listSaleBills: vi.fn() }
})
vi.mock("@/lib/api/invoices", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/invoices")>("@/lib/api/invoices")
  return { ...actual, createInvoice: vi.fn() }
})

const bill = (id: string, no: string, total: string, invoiced: string, st: string) =>
  ({
    id,
    bill_no: no,
    bill_date: "2026-10-10T00:00:00Z",
    total_amount: total,
    invoiced_amount: invoiced,
    invoice_status: st,
    status: 2,
  }) as unknown as saleApi.SaleBillHead

async function openWithBills() {
  vi.mocked(saleApi.listSaleBills).mockResolvedValue({
    items: [bill("b-1", "SL-0001", "200.0000", "50.0000", "partial"), bill("b-2", "SL-0002", "80.0000", "80.0000", "full")],
    total: 2,
  })
  render(<InvoiceRegisterSheet open onOpenChange={() => {}} />)
  fireEvent.click(screen.getByText(/选客户/))
  await screen.findByText("SL-0001")
}

describe("InvoiceRegisterSheet", () => {
  beforeEach(() => {
    vi.mocked(saleApi.listSaleBills).mockReset()
    vi.mocked(invoiceApi.createInvoice).mockReset()
  })

  it("sums the total from excl + tax", () => {
    render(<InvoiceRegisterSheet open onOpenChange={() => {}} />)
    fireEvent.change(screen.getByLabelText(/不含税金额/), { target: { value: "100.10" } })
    fireEvent.change(screen.getByLabelText("税额"), { target: { value: "13.01" } })
    expect(screen.getByTestId("inv-total").textContent).toContain("113.11")
  })

  it("lists only bills not fully invoiced and defaults the allocation to the remainder", async () => {
    await openWithBills()
    expect(screen.queryByText("SL-0002")).toBeNull()
    fireEvent.click(screen.getByLabelText("关联 SL-0001"))
    expect((screen.getByLabelText("SL-0001 关联金额") as HTMLInputElement).value).toBe("150.00")
    expect(vi.mocked(saleApi.listSaleBills).mock.calls[0][0]).toMatchObject({ partner_id: "c-1", status: 2 })
  })

  it("validates required fields and the allocation cap before calling the API", async () => {
    await openWithBills()
    fireEvent.click(screen.getByRole("button", { name: "登记发票" }))
    expect(screen.getByRole("alert").textContent).toContain("请填写发票号码")

    fireEvent.change(screen.getByLabelText(/发票号码/), { target: { value: "No123" } })
    fireEvent.change(screen.getByLabelText(/不含税金额/), { target: { value: "100" } })
    fireEvent.click(screen.getByLabelText("关联 SL-0001"))
    // 150 allocated against a 100 invoice
    fireEvent.click(screen.getByRole("button", { name: "登记发票" }))
    expect(screen.getByRole("alert").textContent).toContain("不能超过")

    fireEvent.change(screen.getByLabelText("SL-0001 关联金额"), { target: { value: "160" } })
    fireEvent.change(screen.getByLabelText(/不含税金额/), { target: { value: "200" } })
    fireEvent.click(screen.getByRole("button", { name: "登记发票" }))
    expect(screen.getByRole("alert").textContent).toContain("最多还能开票")
    expect(invoiceApi.createInvoice).not.toHaveBeenCalled()
  })

  it("rejects amounts with more than two decimals instead of rounding them to zero", async () => {
    await openWithBills()
    fireEvent.change(screen.getByLabelText(/发票号码/), { target: { value: "No1" } })
    fireEvent.change(screen.getByLabelText(/不含税金额/), { target: { value: "0.001" } })
    fireEvent.click(screen.getByRole("button", { name: "登记发票" }))
    expect(screen.getByRole("alert").textContent).toContain("两位小数")
    expect(invoiceApi.createInvoice).not.toHaveBeenCalled()
  })

  it("submits decimal strings with the linked bill", async () => {
    await openWithBills()
    vi.mocked(invoiceApi.createInvoice).mockResolvedValue({ id: "i-1" } as invoiceApi.Invoice)
    fireEvent.change(screen.getByLabelText(/发票号码/), { target: { value: " No123 " } })
    fireEvent.change(screen.getByLabelText(/不含税金额/), { target: { value: "132.74" } })
    fireEvent.change(screen.getByLabelText("税额"), { target: { value: "17.26" } })
    fireEvent.click(screen.getByLabelText("关联 SL-0001"))
    fireEvent.click(screen.getByRole("button", { name: "登记发票" }))
    await waitFor(() => expect(invoiceApi.createInvoice).toHaveBeenCalledTimes(1))
    const body = vi.mocked(invoiceApi.createInvoice).mock.calls[0][0]
    expect(body).toMatchObject({
      direction: "out",
      invoice_no: "No123",
      partner_id: "c-1",
      amount_excl_tax: "132.74",
      tax_amount: "17.26",
      links: [{ bill_id: "b-1", amount: "150.00" }],
    })
  })

  it("ticks the preset bill once the candidates load", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [bill("b-1", "SL-0001", "200.0000", "0", "none")],
      total: 1,
    })
    const preset = { direction: "out" as const, partnerId: "c-1", partnerName: "老王花店", billId: "b-1" }
    render(<InvoiceRegisterSheet open onOpenChange={() => {}} preset={preset} />)
    await waitFor(() => expect((screen.getByLabelText("SL-0001 关联金额") as HTMLInputElement).value).toBe("200.00"))
  })

  it("adds the preset bill from the entry point when it is older than the loaded page", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [bill("b-9", "SL-0009", "100.0000", "0", "none")],
      total: 150,
    })
    const preset = {
      direction: "out" as const,
      partnerId: "c-1",
      partnerName: "老王花店",
      billId: "b-old",
      bill: { billNo: "SL-0001", billDate: "2026-01-02T00:00:00Z", totalAmount: "200.00", remaining: "75.50" },
    }
    render(<InvoiceRegisterSheet open onOpenChange={() => {}} preset={preset} />)
    await waitFor(() => expect((screen.getByLabelText("SL-0001 关联金额") as HTMLInputElement).value).toBe("75.50"))
    expect(screen.queryByText(/已经开足发票/)).toBeNull()
    expect(screen.getByText(/只列出了最近的 100 张/)).toBeTruthy()
  })
})

describe("InvoiceRegisterSheet tax rate and returns", () => {
  beforeEach(() => {
    vi.mocked(saleApi.listSaleBills).mockReset()
  })

  it("works out excl and tax from the tax-inclusive total at the chosen rate, and stays editable", () => {
    render(<InvoiceRegisterSheet open onOpenChange={() => {}} />)
    fireEvent.change(screen.getByLabelText("税率"), { target: { value: "0.03" } })
    fireEvent.change(screen.getByLabelText("价税合计"), { target: { value: "103" } })
    expect((screen.getByLabelText(/不含税金额/) as HTMLInputElement).value).toBe("100.00")
    expect((screen.getByLabelText("税额") as HTMLInputElement).value).toBe("3.00")
    fireEvent.change(screen.getByLabelText("税额"), { target: { value: "3.50" } })
    expect((screen.getByLabelText("价税合计") as HTMLInputElement).value).toBe("103.50")
  })

  it("offers only what is left after sales returns", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [{ ...bill("b-1", "SL-0001", "200.0000", "0.0000", "none"), paid_amount: "0.0000", unpaid_amount: "80.0000" } as unknown as saleApi.SaleBillHead],
      total: 1,
    })
    render(<InvoiceRegisterSheet open onOpenChange={() => {}} />)
    fireEvent.click(screen.getByText(/选客户/))
    await screen.findByText("SL-0001")
    fireEvent.click(screen.getByLabelText("关联 SL-0001"))
    expect((screen.getByLabelText("SL-0001 关联金额") as HTMLInputElement).value).toBe("80.00")
  })
})
