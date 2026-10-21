import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"

const { role } = vi.hoisted(() => ({ role: { v: "cashier" } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: role.v, isOwner: role.v === "owner" } } }),
}))

vi.mock("@/components/cashier-mode-gate", () => ({
  HideInCashierMode: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

import { CheckoutSuccess } from "./checkout-success"

const props = { billNo: "SO-1", totalAmount: "9.90", billId: "bill-1", onDismiss: vi.fn() }

describe("CheckoutSuccess settings link wording", () => {
  it("drops the shop name for a cashier, who cannot edit it", () => {
    role.v = "cashier"
    render(<CheckoutSuccess {...props} />)
    expect(screen.getByText("设置小票纸宽和自动打印")).toBeInTheDocument()
    expect(screen.queryByText(/店名/)).toBeNull()
  })

  it("keeps the shop name for the owner", () => {
    role.v = "owner"
    render(<CheckoutSuccess {...props} />)
    expect(screen.getByText("设置店名、小票纸宽和自动打印")).toBeInTheDocument()
  })
})
