import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { InvoiceTag } from "./bill-list-filters"

describe("InvoiceTag", () => {
  it("shows the Chinese label per status on approved bills", () => {
    const { unmount } = render(<InvoiceTag status="none" billStatus={2} />)
    expect(screen.getByText("未开票")).toBeTruthy()
    unmount()
    const r2 = render(<InvoiceTag status="partial" billStatus={2} />)
    expect(screen.getByText("部分开票")).toBeTruthy()
    r2.unmount()
    render(<InvoiceTag status="full" billStatus={2} />)
    expect(screen.getByText("已开票")).toBeTruthy()
  })

  it("shows a dash for non-approved bills", () => {
    render(<InvoiceTag status="full" billStatus={0} />)
    expect(screen.getByText("—")).toBeTruthy()
  })

  it("renders nothing for a missing or unknown status", () => {
    const { container } = render(<InvoiceTag billStatus={2} />)
    expect(container.textContent).toBe("")
  })
})
