import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import Decimal from "decimal.js"

vi.mock("@/components/entity-pickers", () => ({ CustomerPicker: () => null }))
const lock = vi.hoisted(() => ({ locked: false }))
vi.mock("@/lib/api/billing", () => ({
  getSubscriptionLockState: vi.fn(async () => ({ locked: lock.locked })),
}))

import { PaymentModal } from "./payment-modal"

describe("PaymentModal with a lapsed plan", () => {
  beforeEach(() => {
    lock.locked = false
  })
  const props = { open: true, totalAmount: new Decimal("10"), onConfirm: vi.fn(), onClose: vi.fn() }

  it("disables the confirm button and explains why", async () => {
    lock.locked = true
    render(<PaymentModal {...props} mode="cash" />)
    expect(await screen.findByText("套餐已到期，现在只能查看，请联系老板续费")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "确认收款" })).toBeDisabled()
  })

  it("keeps the button enabled while the plan is active", async () => {
    render(<PaymentModal {...props} mode="cash" />)
    await waitFor(() => expect(screen.queryByText(/套餐已到期/)).toBeNull())
    expect(screen.getByRole("button", { name: "确认收款" })).toBeEnabled()
  })
})
