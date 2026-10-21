import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { StatementTable } from "./statement-table"
import type { Statement } from "@/lib/api/receipts"

const base: Statement = {
  direction: "ar",
  partner_id: "c-1",
  partner_name: "老客户",
  from: "2026-10-01",
  to: "2026-10-31",
  opening: "100",
  billed: "200",
  returned: "0",
  paid: "50",
  adjustment: "0",
  closing: "250",
  lines: [
    { date: "2026-10-02T00:00:00+08:00", kind: "bill", bill_no: "SL-1", delta: "200", balance: "300" },
    { date: "2026-10-03T00:00:00+08:00", kind: "payment", bill_no: "SL-1", method: "cash", delta: "-50", balance: "250" },
    { date: "2026-10-04T00:00:00+08:00", kind: "payment", bill_no: "SL-1", method: "cash", remark: "多收退回", delta: "30", balance: "280" },
  ],
}

describe("StatementTable", () => {
  it("labels a payment that raises the balance as a refund and shows signed amounts", () => {
    render(<StatementTable st={base} />)
    expect(screen.getByText("退款")).toBeInTheDocument()
    expect(screen.getByText("发货")).toBeInTheDocument()
    expect(screen.getByText("-¥50.00")).toBeInTheDocument()
    expect(screen.queryByText("其他调整")).not.toBeInTheDocument()
  })

  it("names a supplier-side return offset instead of calling it a payment", () => {
    const st: Statement = {
      ...base,
      direction: "ap",
      returned: "20",
      lines: [{ date: "2026-10-05T00:00:00+08:00", kind: "return", bill_no: "PO-1", delta: "-20", balance: "80" }],
    }
    render(<StatementTable st={st} />)
    expect(screen.getByText("退货冲抵")).toBeInTheDocument()
    expect(screen.queryByText("赊账")).not.toBeInTheDocument()
  })
})
