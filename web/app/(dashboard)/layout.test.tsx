import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen } from "@testing-library/react"

// Mock next/link and next/navigation
vi.mock("next/link", () => ({
  default: ({
    href,
    children,
    ...props
  }: {
    href: string
    children: React.ReactNode
    [key: string]: unknown
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))

vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard",
  useRouter: () => ({ push: vi.fn() }),
}))

// Mock useProfile to control profile type
const mockUseProfile = vi.fn()
vi.mock("@/lib/profile", () => ({
  useProfile: () => mockUseProfile(),
  ProfileProvider: ({ children }: { children: React.ReactNode }) => children,
}))

// We test a client wrapper component that wraps the sidebar logic
// The actual layout.tsx is a Server Component; we test the Sidebar client component
vi.mock("@/auth", () => ({ auth: async () => null }))
vi.mock("@/components/ai-assistant/GlobalAI", () => ({ GlobalAI: () => null }))
vi.mock("@/components/account/account-drawer", () => ({ AccountDrawer: () => null }))
vi.mock("@/components/undo/UndoToastProvider", () => ({ UndoToastProvider: ({ children }: { children: React.ReactNode }) => children }))
vi.mock("@/components/ui/offline-banner", () => ({ OfflineBanner: () => null }))
vi.mock("@/components/ui/route-transition", () => ({ RouteTransition: ({ children }: { children: React.ReactNode }) => children }))
import DashboardLayout from "./layout"
import { DashboardSidebar } from "./sidebar"

describe("DashboardSidebar", () => {
  it("TestDashboardSidebar_RetailProfile_ShowsPosLink: retail profile shows POS link", async () => {
    mockUseProfile.mockReturnValue({ profileType: "retail" })

    render(<DashboardSidebar />)

    const posLink = await screen.findByRole("link", { name: /POS 收银/i })
    expect(posLink).toBeInTheDocument()
    expect(posLink.getAttribute("href")).toBe("/pos")
  })

  it("TestDashboardSidebar_CrossBorderProfile_HidesPosLink: cross_border profile hides POS link", () => {
    mockUseProfile.mockReturnValue({ profileType: "cross_border" })

    render(<DashboardSidebar />)

    expect(screen.queryByRole("link", { name: /POS 收银/i })).not.toBeInTheDocument()
  })

  it("TestDashboardSidebar_HybridProfile_HidesPosLink: hybrid profile hides POS link", () => {
    mockUseProfile.mockReturnValue({ profileType: "hybrid" })

    render(<DashboardSidebar />)

    expect(screen.queryByRole("link", { name: /POS 收银/i })).not.toBeInTheDocument()
  })

  it("TestDashboardSidebar_AlwaysShowsProductsLink: products link is always visible", () => {
    mockUseProfile.mockReturnValue({ profileType: "cross_border" })

    render(<DashboardSidebar />)

    expect(screen.getByRole("link", { name: /商品/ })).toBeInTheDocument()
  })

  it("TestDashboardSidebar_HorticultureProfile_ShowsDictionaryLink: horticulture shows 苗木字典", () => {
    mockUseProfile.mockReturnValue({ profileType: "horticulture" })

    render(<DashboardSidebar />)

    expect(screen.getByRole("link", { name: /苗木字典/ })).toBeInTheDocument()
  })

  it("TestDashboardSidebar_CrossBorderProfile_HidesDictionaryLink: cross_border hides 苗木字典", () => {
    mockUseProfile.mockReturnValue({ profileType: "cross_border" })

    render(<DashboardSidebar />)

    expect(screen.queryByRole("link", { name: /苗木字典/ })).not.toBeInTheDocument()
  })

  it("TestDashboardSidebar_RetailProfile_HidesDictionaryLink: retail hides 苗木字典", () => {
    mockUseProfile.mockReturnValue({ profileType: "retail" })

    render(<DashboardSidebar />)

    expect(screen.queryByRole("link", { name: /苗木字典/ })).not.toBeInTheDocument()
  })

  it("TestDashboardSidebar_HorticultureProfile_ShowsProjectsLink: projects is core and always shown", () => {
    mockUseProfile.mockReturnValue({ profileType: "horticulture" })

    render(<DashboardSidebar />)

    expect(screen.getByRole("link", { name: /项目/ })).toBeInTheDocument()
  })
})

describe("DashboardLayout shared device", () => {
  afterEach(() => window.localStorage.clear())

  it("leaves for the switch login when another tab signs out", async () => {
    mockUseProfile.mockReturnValue({ profileType: "retail" })
    const assign = vi.fn()
    const orig = window.location
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } })
    try {
      render(await DashboardLayout({ children: <div>后台页</div> }))
      window.dispatchEvent(new StorageEvent("storage", { key: "tally:signed-out-at", newValue: "123" }))
      expect(assign).toHaveBeenCalledWith("/login?switch=1")
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: orig })
    }
  })
})
