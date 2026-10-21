import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "owner", isOwner: true } }, status: "authenticated" }),
}))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const api = vi.hoisted(() => ({ listCustomers: vi.fn() }))
vi.mock("@/lib/api/customers", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/customers")>()),
  ...api,
}))
const recv = vi.hoisted(() => ({ getReceivables: vi.fn() }))
vi.mock("@/lib/api/receivables", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/receivables")>()),
  ...recv,
}))

import CustomersPage from "./page"

const zero = { unpaid: "0", b0_30: "0", b31_60: "0", b61_90: "0", b90p: "0", oldest_bill_date: "" }

describe("CustomersPage 欠款 column", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.listCustomers.mockResolvedValue({
      items: [
        { id: "c-1", tenant_id: "t", name: "欠款客户" },
        { id: "c-2", tenant_id: "t", name: "清账客户" },
      ],
      total: 2,
    })
  })

  it("shows the amount as a link for a debtor and 无欠款 for the rest", async () => {
    recv.getReceivables.mockResolvedValue({
      direction: "ar",
      as_of: "2026-10-10",
      items: [{ ...zero, partner_id: "c-1", partner_name: "欠款客户", unpaid: "320.5" }],
      total: { ...zero, partner_id: null, partner_name: "合计", unpaid: "320.5" },
    })
    render(<CustomersPage />)
    const link = await screen.findByText("¥320.50")
    expect(link.closest("a")?.getAttribute("href")).toContain("/payments?partner=c-1")
    expect(screen.getByText("无欠款")).toBeInTheDocument()
  })

  it("falls back to a dash when the summary request fails", async () => {
    recv.getReceivables.mockRejectedValue(new Error("boom"))
    render(<CustomersPage />)
    await waitFor(() => expect(screen.getByText("清账客户")).toBeInTheDocument())
    await waitFor(() => expect(recv.getReceivables).toHaveBeenCalled())
    expect(screen.queryByText("无欠款")).not.toBeInTheDocument()
    expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(2)
  })
})
