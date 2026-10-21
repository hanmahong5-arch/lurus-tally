import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "owner", isOwner: true } } }),
  signOut: vi.fn(),
}))
vi.mock("next/image", () => ({ default: () => null }))
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("./load-summary", () => ({
  loadAccountSummary: vi.fn().mockResolvedValue({
    identity: { user_id: "u", tenant_id: "12345678-aaaa-bbbb-cccc-000000000000", email: "a@x.com", display_name: "小李", role: "owner", is_owner: true, profile_type: "retail" },
    billing: { subscription: { plan_code: "free", status: "active" }, wallet: { available: 10, frozen: 0, total: 10 }, entitlements: {} },
    billingError: null,
  }),
}))
vi.mock("./account-drawer-provider", () => ({
  useAccountDrawer: () => ({ open: true, openDrawer: vi.fn(), closeDrawer: vi.fn() }),
}))

import { AccountDrawer } from "./account-drawer"

describe("account drawer in the trial period", () => {
  it("shows the wallet but hides the top-up entry", async () => {
    const { container } = render(<AccountDrawer />)
    await screen.findByText("钱包余额")
    expect(container.textContent).not.toContain("充值 →")
  })
})
