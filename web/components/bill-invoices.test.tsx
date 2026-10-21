import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import * as api from "@/lib/api/invoices"
import { BillInvoices } from "./bill-invoices"

vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
const seenPresets: unknown[] = []
vi.mock("@/components/invoice-register-sheet", () => ({
  InvoiceRegisterSheet: ({ open, preset }: { open: boolean; preset?: { billId?: string } }) => {
    if (open) seenPresets.push(preset)
    return open ? <div>抽屉已打开:{preset?.billId}</div> : null
  },
}))
vi.mock("@/lib/api/invoices", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/invoices")>("@/lib/api/invoices")
  return { ...actual, listInvoices: vi.fn() }
})

const inv = (id: string, no: string, billId: string, amount: string) =>
  ({
    id,
    invoice_no: no,
    invoice_type: "数电普票",
    issue_date: "2026-10-10",
    links: [
      { bill_id: billId, bill_no: "SL-1", amount },
      { bill_id: "b-other", bill_no: "SL-2", amount: "9.00" },
    ],
  }) as unknown as api.Invoice

describe("BillInvoices", () => {
  beforeEach(() => {
    vi.mocked(api.listInvoices).mockReset()
  })

  it("queries by bill in one call, shows this bill's share, and opens the drawer with the bill preselected", async () => {
    vi.mocked(api.listInvoices).mockResolvedValue({ items: [inv("i-1", "FP0001", "b-1", "120.00")], total: 1 })
    render(<BillInvoices kind="sale" billId="b-1" partnerId="c-1" partnerName="老王花店" approved totalAmount="300.00" />)
    expect(await screen.findByText("FP0001")).toBeTruthy()
    expect(screen.getByText(/¥120\.00/)).toBeTruthy()
    expect(api.listInvoices).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.listInvoices).mock.calls[0][0]).toMatchObject({ bill_id: "b-1", status: "issued" })
    fireEvent.click(screen.getByRole("button", { name: "登记发票" }))
    expect(screen.getByText("抽屉已打开:b-1")).toBeTruthy()
  })

  it("hides the register shortcut once the linked amounts cover the bill total", async () => {
    vi.mocked(api.listInvoices).mockResolvedValue({ items: [inv("i-1", "FP0001", "b-1", "300.00")], total: 1 })
    render(<BillInvoices kind="sale" billId="b-1" partnerId="c-1" approved totalAmount="300.00" />)
    expect(await screen.findByText("FP0001")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "登记发票" })).toBeNull()
  })

  it("shows the empty hint and keeps the shortcut for an uninvoiced bill", async () => {
    vi.mocked(api.listInvoices).mockResolvedValue({ items: [], total: 0 })
    render(<BillInvoices kind="sale" billId="b-1" partnerId="c-1" approved totalAmount="300.00" />)
    expect(await screen.findByText("还没有登记发票")).toBeTruthy()
    expect(screen.getByRole("button", { name: "登记发票" })).toBeTruthy()
  })

  it("renders nothing for a bill that is not approved", () => {
    const { container } = render(<BillInvoices kind="purchase" billId="b-1" partnerId="s-1" approved={false} />)
    expect(container.innerHTML).toBe("")
    expect(api.listInvoices).not.toHaveBeenCalled()
  })

  it("disables 登记发票 until the invoice list has loaded, then opens with a stable preset", async () => {
    let resolve!: (v: { items: api.Invoice[]; total: number }) => void
    vi.mocked(api.listInvoices).mockReturnValue(new Promise((r) => (resolve = r)))
    seenPresets.length = 0
    render(<BillInvoices kind="sale" billId="b-1" partnerId="c-1" partnerName="老王花店" approved billNo="SL-1" billDate="2026-10-10" totalAmount="300.00" />)
    expect((screen.getByRole("button", { name: "登记发票" }) as HTMLButtonElement).disabled).toBe(true)
    resolve({ items: [inv("i-1", "FP0001", "b-1", "120.00")], total: 1 })
    expect(await screen.findByText("FP0001")).toBeTruthy()
    await waitFor(() => expect((screen.getByRole("button", { name: "登记发票" }) as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "登记发票" }))
    await waitFor(() => expect(seenPresets.length).toBeGreaterThan(0))
    expect(new Set(seenPresets).size).toBe(1)
  })
})

describe("BillInvoices net of returns", () => {
  it("hides the register shortcut once the invoices cover what is left after returns", async () => {
    vi.mocked(api.listInvoices).mockResolvedValue({ items: [inv("i-1", "FP0001", "b-1", "200.00")], total: 1 })
    render(<BillInvoices kind="sale" billId="b-1" partnerId="c-1" approved totalAmount="300.00" returnedAmount="100.00" />)
    expect(await screen.findByText("FP0001")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "登记发票" })).toBeNull()
  })
})
