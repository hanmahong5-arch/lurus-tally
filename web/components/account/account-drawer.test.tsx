import { describe, it, expect, vi } from "vitest"
import { render, screen, act } from "@testing-library/react"

vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: "clerk", isOwner: false } } }),
  signOut: vi.fn(),
}))
vi.mock("next/image", () => ({ default: () => null }))
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("./load-summary", () => ({
  loadAccountSummary: vi.fn().mockResolvedValue({ identity: { user_id: "u", tenant_id: "12345678-aaaa-bbbb-cccc-000000000000", email: "a@x.com", display_name: "小李", role: "clerk", is_owner: false, profile_type: "retail" }, billing: null, billingError: null }),
}))
vi.mock("./account-drawer-provider", () => ({
  useAccountDrawer: () => ({ open: true, openDrawer: vi.fn(), closeDrawer: vi.fn() }),
}))

import { AccountDrawer } from "./account-drawer"

describe("AccountDrawer install entry", () => {
  it("renders the install entry once the browser offers an install prompt", async () => {
    window.matchMedia = vi.fn().mockImplementation((q: string) => ({
      matches: false,
      media: q,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })) as unknown as typeof window.matchMedia
    render(<AccountDrawer />)
    expect(screen.queryByTestId("install-button")).toBeNull()
    const ev = Object.assign(new Event("beforeinstallprompt", { cancelable: true }), {
      prompt: vi.fn().mockResolvedValue(undefined),
    })
    act(() => {
      window.dispatchEvent(ev)
    })
    expect(await screen.findByTestId("install-button")).toBeInTheDocument()
  })
})
