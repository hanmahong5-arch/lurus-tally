import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

const { state } = vi.hoisted(() => ({ state: { role: "owner" as string } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: state.role, isOwner: state.role === "owner" } } }),
}))
vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: "retail" }),
  ProfileGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))
vi.mock("@/components/unit-selector", () => ({ UnitSelector: () => <select /> }))
vi.mock("@/components/cross-border/hs-code-input", () => ({ HsCodeInput: () => <input /> }))
vi.mock("@/lib/api/products", () => ({ listProducts: async () => ({ items: [], total: 0 }) }))

import { ProductForm } from "./product-form"

describe("ProductForm by role", () => {
  beforeEach(() => {
    state.role = "owner"
  })

  it("shows the purchase price box to someone who may see cost", () => {
    state.role = "manager"
    render(<ProductForm onSubmit={vi.fn()} />)
    expect(screen.getByLabelText("进价")).toBeInTheDocument()
  })

  it("does not draw or submit the purchase price for a clerk", async () => {
    state.role = "clerk"
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <ProductForm
        onSubmit={onSubmit}
        initial={{ id: "p1", name: "螺丝", purchase_price: "8" } as never}
      />,
    )
    expect(screen.queryByLabelText("进价")).toBeNull()
    expect(screen.getByLabelText("零售价")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "保存" }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    expect(onSubmit.mock.calls[0][0].purchase_price).toBeUndefined()
  })
})
