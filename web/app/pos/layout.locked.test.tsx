import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("next/navigation", () => ({ usePathname: () => "/pos", useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }))
vi.mock("@/components/ui/offline-banner", () => ({ OfflineBanner: () => null }))
vi.mock("@/components/pos/pos-header-actions", () => ({ BackOfficeLink: () => null, SignOutButton: () => null }))
vi.mock("@/components/cashier-mode-gate", () => ({
  CashierModeControl: () => null,
  CashierModeProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CashierModeReadNotice: () => null,
  HideInCashierMode: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "cashier", isOwner: false } } }),
}))
const lock = vi.hoisted(() => ({ locked: false }))
vi.mock("@/lib/api/billing", () => ({
  getSubscriptionLockState: vi.fn(async () => ({ locked: lock.locked })),
  getSubscriptionState: vi.fn(),
}))

import POSLayout from "./layout"

describe("POS layout subscription banner", () => {
  beforeEach(() => {
    lock.locked = false
  })

  it("shows the lapsed-plan banner for a cashier", async () => {
    lock.locked = true
    render(await POSLayout({ children: <div>pos</div> }))
    expect(await screen.findByRole("alert")).toHaveTextContent("套餐已到期")
    expect(screen.getByText("请联系老板续费")).toBeInTheDocument()
  })

  it("shows nothing while the plan is active", async () => {
    render(await POSLayout({ children: <div>pos</div> }))
    await screen.findByText("pos")
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByRole("alert")).toBeNull()
  })
})
