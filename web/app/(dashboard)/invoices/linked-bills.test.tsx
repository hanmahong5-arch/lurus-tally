import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { LinkedBills } from "./linked-bills"

const link = (no: string) => ({ bill_id: "x-" + no, bill_no: no, amount: "1.00" })

describe("LinkedBills", () => {
  it("shows a dash when the invoice covers no bill", () => {
    render(<LinkedBills links={[]} />)
    expect(screen.getByText("—")).toBeTruthy()
  })

  it("lists bill numbers and never the internal id", () => {
    render(<LinkedBills links={[link("SO-1"), link("SO-2")]} />)
    expect(screen.getByTestId("linked-bills").textContent).toBe("SO-1、SO-2")
  })

  it("collapses long lists into the first two plus a count", () => {
    render(<LinkedBills links={[link("SO-1"), link("SO-2"), link("SO-3"), link("SO-4")]} />)
    expect(screen.getByTestId("linked-bills").textContent).toContain("SO-1、SO-2")
    expect(screen.getByTestId("linked-bills").textContent).toContain("等 4 张")
  })
})
