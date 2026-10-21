import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

const { role, listAudit } = vi.hoisted(() => ({ role: { v: "owner" }, listAudit: vi.fn() }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: role.v, isOwner: role.v === "owner", tenantId: "t1" } } }),
}))
vi.mock("@/lib/api/audit", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/audit")>("@/lib/api/audit")
  return { ...actual, listAudit }
})

import AuditPage from "./page"

const row = (i: number, over: Record<string, string> = {}) => ({
  id: `a${i}`,
  at: "2026-10-14T10:00:00Z",
  actor_name: "老王",
  action: "bill.approved",
  action_label: "审核",
  resource: "purchase_bill",
  resource_label: "采购单",
  resource_id: "uuid-hidden",
  resource_no: "PO-20261014-0001",
  summary: "",
  ...over,
})

beforeEach(() => {
  role.v = "owner"
  listAudit.mockReset()
  listAudit.mockResolvedValue({ items: [row(1), row(2, { action_label: "作废", summary: "原因：录错了" })], total: 2 })
})

describe("audit page", () => {
  it("renders the table in Chinese without ids or english actions", async () => {
    render(<AuditPage />)
    expect((await screen.findAllByText("PO-20261014-0001", { exact: false })).length).toBe(2)
    for (const h of ["时间", "操作人", "动作", "对象", "说明"]) expect(screen.getByText(h)).toBeInTheDocument()
    expect(screen.getAllByText("老王")).toHaveLength(2)
    expect(screen.getByText("作废")).toBeInTheDocument()
    expect(screen.getByText("原因：录错了")).toBeInTheDocument()
    expect(screen.queryByText(/uuid-hidden|bill\.approved/)).toBeNull()
  })

  it("filters by object type and goes back to page one", async () => {
    render(<AuditPage />)
    await screen.findByText("时间")
    fireEvent.change(screen.getByLabelText("对象类型"), { target: { value: "sale_bill" } })
    await waitFor(() =>
      expect(listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ resource: "sale_bill", offset: 0 })),
    )
  })

  it("pages through results", async () => {
    listAudit.mockResolvedValue({ items: [row(1)], total: 45 })
    render(<AuditPage />)
    await screen.findByText("时间")
    fireEvent.click(screen.getByRole("button", { name: "下一页" }))
    await waitFor(() => expect(listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 20 })))
  })

  it("shows the empty state", async () => {
    listAudit.mockResolvedValue({ items: [], total: 0 })
    render(<AuditPage />)
    expect(await screen.findByText("还没有操作记录")).toBeInTheDocument()
  })

  it("shows a Chinese error", async () => {
    listAudit.mockRejectedValue(new Error("boom"))
    render(<AuditPage />)
    expect(await screen.findByRole("alert")).toBeInTheDocument()
  })

  it("refuses a clerk without calling the server", async () => {
    role.v = "clerk"
    render(<AuditPage />)
    expect(await screen.findByText("你的角色没有查看操作记录的权限")).toBeInTheDocument()
    expect(listAudit).not.toHaveBeenCalled()
  })
})
