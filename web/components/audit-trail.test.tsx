import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"

const { listAudit } = vi.hoisted(() => ({ listAudit: vi.fn() }))
vi.mock("@/lib/api/audit", () => ({ listAudit }))

import { AuditTrail } from "./audit-trail"

beforeEach(() => {
  listAudit.mockReset()
  listAudit.mockResolvedValue({
    items: [
      { id: "1", at: "2026-10-14T10:00:00Z", actor_name: "老王", action_label: "审核", summary: "" },
      { id: "2", at: "2026-10-14T09:00:00Z", actor_name: "小李", action_label: "新建", summary: "单价 12.00 → 10.00" },
    ],
    total: 2,
  })
})

describe("AuditTrail", () => {
  it("is collapsed and silent until opened", () => {
    render(<AuditTrail resourceId="bill-1" />)
    expect(screen.getByText("操作记录")).toBeInTheDocument()
    expect(listAudit).not.toHaveBeenCalled()
  })

  it("loads this document's history when opened", async () => {
    render(<AuditTrail resourceId="bill-1" />)
    fireEvent.click(screen.getByRole("button", { name: /操作记录/ }))
    expect(await screen.findByText(/老王/)).toBeInTheDocument()
    expect(screen.getByText(/小李/)).toBeInTheDocument()
    expect(screen.getByText(/单价 12.00 → 10.00/)).toBeInTheDocument()
    expect(listAudit).toHaveBeenCalledWith(expect.objectContaining({ resourceId: "bill-1" }))
  })

  it("says so when there is no record", async () => {
    listAudit.mockResolvedValue({ items: [], total: 0 })
    render(<AuditTrail resourceId="bill-1" />)
    fireEvent.click(screen.getByRole("button", { name: /操作记录/ }))
    expect(await screen.findByText("这张单还没有操作记录。")).toBeInTheDocument()
  })
})
