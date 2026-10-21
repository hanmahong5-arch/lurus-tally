import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import * as api from "@/lib/api/invoices"
import InvoicesPage from "./page"

const confirmMock = vi.fn()
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => confirmMock }))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
vi.mock("@/hooks/use-role", () => ({ useRole: () => ({ role: "owner", known: true, can: () => true }) }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("@/lib/api/customers", () => ({ listCustomers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/lib/api/suppliers", () => ({ listSuppliers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/components/invoice-register-sheet", () => ({ InvoiceRegisterSheet: () => null }))
vi.mock("@/lib/api/invoices", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/invoices")>("@/lib/api/invoices")
  return { ...actual, listInvoices: vi.fn(), voidInvoice: vi.fn(), redInvoice: vi.fn() }
})

const inv = (over: Partial<api.Invoice>): api.Invoice => ({
  id: "i-1",
  direction: "out",
  invoice_no: "FP0001",
  invoice_code: "",
  invoice_type: "数电普票",
  partner_id: "c-1",
  partner_name: "老王花店",
  issue_date: "2026-10-10",
  amount_excl_tax: "100.00",
  tax_amount: "13.00",
  total_amount: "113.00",
  status: "issued",
  red_of_id: null,
  remark: "",
  created_at: "",
  updated_at: "",
  links: [],
  ...over,
})

const summary = { count: 1, amount_excl_tax: "100.00", tax_amount: "13.00", total_amount: "113.00" }

describe("InvoicesPage", () => {
  beforeEach(() => {
    vi.mocked(api.listInvoices).mockReset()
    vi.mocked(api.voidInvoice).mockReset()
    vi.mocked(api.redInvoice).mockReset()
    confirmMock.mockReset()
    vi.mocked(api.listInvoices).mockResolvedValue({ items: [inv({})], total: 1, summary })
  })

  it("shows names and amounts, never ids, plus the summary bar", async () => {
    render(<InvoicesPage />)
    expect(await screen.findByText("FP0001")).toBeTruthy()
    expect(screen.getByText("老王花店")).toBeTruthy()
    expect(screen.getByTestId("invoice-summary").textContent).toContain("¥113.00")
    expect(document.body.textContent).not.toContain("i-1")
    expect(document.body.textContent).not.toContain("c-1")
  })

  it("filters by direction through the API", async () => {
    render(<InvoicesPage />)
    await screen.findByText("FP0001")
    fireEvent.click(screen.getByRole("tab", { name: /^销项/ }))
    await waitFor(() => expect(vi.mocked(api.listInvoices).mock.calls.at(-1)?.[0]).toMatchObject({ direction: "out" }))
  })

  it("voids after confirmation and reloads", async () => {
    confirmMock.mockResolvedValue(true)
    vi.mocked(api.voidInvoice).mockResolvedValue(inv({ status: "void" }))
    render(<InvoicesPage />)
    await screen.findByText("FP0001")
    fireEvent.click(screen.getByRole("button", { name: "作废 FP0001" }))
    await waitFor(() => expect(api.voidInvoice).toHaveBeenCalledWith("i-1", "t-1"))
    await waitFor(() => expect(vi.mocked(api.listInvoices).mock.calls.length).toBeGreaterThan(1))
  })

  it("does not void when the confirmation is declined", async () => {
    confirmMock.mockResolvedValue(false)
    render(<InvoicesPage />)
    await screen.findByText("FP0001")
    fireEvent.click(screen.getByRole("button", { name: "作废 FP0001" }))
    await waitFor(() => expect(confirmMock).toHaveBeenCalled())
    expect(api.voidInvoice).not.toHaveBeenCalled()
  })

  it("red-letters with a required new number", async () => {
    vi.mocked(api.redInvoice).mockResolvedValue(inv({ id: "i-2", total_amount: "-113.00" }))
    render(<InvoicesPage />)
    await screen.findByText("FP0001")
    fireEvent.click(screen.getByRole("button", { name: "红冲 FP0001" }))
    fireEvent.click(await screen.findByRole("button", { name: "确认红冲" }))
    expect((await screen.findByRole("alert")).textContent).toContain("红字发票号码")
    expect(api.redInvoice).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("红字发票号码"), { target: { value: "HZ0001" } })
    fireEvent.click(screen.getByRole("button", { name: "确认红冲" }))
    await waitFor(() => expect(api.redInvoice).toHaveBeenCalledTimes(1))
    expect(vi.mocked(api.redInvoice).mock.calls[0][0]).toBe("i-1")
    expect(vi.mocked(api.redInvoice).mock.calls[0][1]).toMatchObject({ invoice_no: "HZ0001" })
  })
})
