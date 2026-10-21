import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "owner", isOwner: true } }, status: "authenticated" }),
}))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const api = vi.hoisted(() => ({ listSuppliers: vi.fn() }))
vi.mock("@/lib/api/suppliers", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/suppliers")>()),
  ...api,
}))
const recv = vi.hoisted(() => ({ getReceivables: vi.fn() }))
vi.mock("@/lib/api/receivables", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/receivables")>()),
  ...recv,
}))

import SuppliersPage from "./page"

const zero = { unpaid: "0", b0_30: "0", b31_60: "0", b61_90: "0", b90p: "0", oldest_bill_date: "" }

describe("SuppliersPage 欠供应商款 column", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.listSuppliers.mockResolvedValue({
      items: [
        { id: "s-1", tenantId: "t", code: "S1", name: "绿源苗圃", contact: "", phone: "", email: "", address: "" },
        { id: "s-2", tenantId: "t", code: "S2", name: "已结清供应商", contact: "", phone: "", email: "", address: "" },
      ],
      total: 2,
    })
  })

  it("links a creditor to its payables tab and shows 无欠款 for the rest", async () => {
    recv.getReceivables.mockResolvedValue({
      direction: "ap",
      as_of: "2026-10-10",
      items: [{ ...zero, partner_id: "s-1", partner_name: "绿源苗圃", unpaid: "230" }],
      total: { ...zero, partner_id: null, partner_name: "合计", unpaid: "230" },
    })
    render(<SuppliersPage />)
    const link = await screen.findByText("¥230.00")
    const href = link.closest("a")?.getAttribute("href") ?? ""
    expect(href).toContain("partner=s-1")
    expect(href).toContain("tab=purchases")
    expect(screen.getByText("无欠款")).toBeInTheDocument()
  })

  it("falls back to a dash when the summary request fails", async () => {
    recv.getReceivables.mockRejectedValue(new Error("boom"))
    render(<SuppliersPage />)
    await waitFor(() => expect(screen.getByText("已结清供应商")).toBeInTheDocument())
    await waitFor(() => expect(recv.getReceivables).toHaveBeenCalled())
    expect(screen.queryByText("无欠款")).not.toBeInTheDocument()
    expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(2)
  })
})
