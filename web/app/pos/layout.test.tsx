import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("next/navigation", () => ({ usePathname: () => "/pos", useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }))
vi.mock("@/components/ui/offline-banner", () => ({ OfflineBanner: () => null }))
const getCashierMode = vi.fn(async () => ({ has_pin: true, tenant_id: "t1" }))
vi.mock("@/lib/api/cashier-mode", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/cashier-mode")>("@/lib/api/cashier-mode")
  return { ...actual, getCashierMode: () => getCashierMode(), setCashierPin: vi.fn(), verifyCashierPin: vi.fn() }
})
import { writeCashierFlag } from "@/lib/api/cashier-mode"

const { roleState, signOutMock } = vi.hoisted(() => ({ roleState: { role: "owner" as string }, signOutMock: vi.fn() }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: roleState.role, isOwner: roleState.role === "owner" } } }),
  signOut: (...a: unknown[]) => signOutMock(...a),
}))

import POSLayout from "./layout"

describe("POS layout", () => {
  beforeEach(() => {
    roleState.role = "owner"
    signOutMock.mockClear()
  })

  it("lets a cashier sign out and hides the back-office exit", async () => {
    window.localStorage.clear()
    roleState.role = "cashier"
    render(await POSLayout({ children: <div>pos</div> }))
    window.sessionStorage.setItem("tally.pos.cart.v1", "{}")
    fireEvent.click(await screen.findByRole("button", { name: "退出登录" }))
    expect(signOutMock).toHaveBeenCalledWith({ redirect: false, redirectTo: "/login?switch=1" })
    await waitFor(() => expect(window.sessionStorage.getItem("tally.pos.cart.v1")).toBeNull())
    expect(screen.queryByText("退出 POS ←")).toBeNull()
  })

  it("keeps the back-office exit and sign out for the owner", async () => {
    window.localStorage.clear()
    render(await POSLayout({ children: <div>pos</div> }))
    expect(await screen.findByText("退出 POS ←")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "退出登录" })).toBeInTheDocument()
  })

  it("offers the way back to the back office when cashier mode is off", async () => {
    window.localStorage.clear()
    render(await POSLayout({ children: <div>pos</div> }))
    expect(await screen.findByText("退出 POS ←")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "退出收银模式" })).toBeNull()
  })

  it("replaces it with the PIN exit in cashier mode, keeping today's records", async () => {
    writeCashierFlag("t1", true)
    render(await POSLayout({ children: <div>pos</div> }))
    expect(await screen.findByRole("button", { name: "退出收银模式" })).toBeInTheDocument()
    expect(screen.queryByText("退出 POS ←")).toBeNull()
    expect(screen.getByText("今日结单")).toBeInTheDocument()
  })

  it("reads the mode once for the header buttons", async () => {
    getCashierMode.mockClear()
    writeCashierFlag("t1", true)
    render(await POSLayout({ children: <div>pos</div> }))
    await screen.findByRole("button", { name: "退出收银模式" })
    expect(getCashierMode).toHaveBeenCalledTimes(1)
  })

  it("says so when the mode cannot be read instead of hiding the buttons silently", async () => {
    window.localStorage.clear()
    getCashierMode.mockRejectedValueOnce(new Error("down"))
    render(await POSLayout({ children: <div>pos</div> }))
    expect(await screen.findByText(/请刷新页面/)).toBeInTheDocument()
  })
})
