import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"

const { authMock, sessionProps } = vi.hoisted(() => ({
  authMock: vi.fn(),
  sessionProps: { last: undefined as unknown },
}))
vi.mock("@/auth", () => ({ auth: () => authMock() }))
vi.mock("next-auth/react", () => ({
  SessionProvider: ({ session, children }: { session?: unknown; children: React.ReactNode }) => {
    sessionProps.last = session
    return <div data-testid="session-provider">{children}</div>
  },
}))
vi.mock("next-themes", () => ({ ThemeProvider: ({ children }: { children: React.ReactNode }) => <>{children}</> }))
vi.mock("@/components/providers/toast-provider", () => ({ ToastProvider: ({ children }: { children: React.ReactNode }) => <>{children}</> }))
vi.mock("@/hooks/useConfirm", () => ({ ConfirmProvider: ({ children }: { children: React.ReactNode }) => <>{children}</> }))
vi.mock("@/app/globals.css", () => ({}))

import RootLayout from "./layout"

describe("RootLayout", () => {
  it("hands the server-side session to the session provider so the role is known on first render", async () => {
    const session = { user: { role: "cashier", isOwner: false }, expires: "2099-01-01" }
    authMock.mockResolvedValue(session)
    const ui = await RootLayout({ children: <span>内容</span> })
    // <html> cannot be mounted into a div; render the body subtree only.
    const body = (ui.props.children as React.ReactElement).props.children
    render(body)
    expect(screen.getByText("内容")).toBeInTheDocument()
    expect(sessionProps.last).toEqual(session)
  })

  it("never hands the access token to client code", async () => {
    authMock.mockResolvedValue({ user: { role: "owner", isOwner: true }, accessToken: "secret-token", expires: "2099-01-01" })
    const ui = await RootLayout({ children: <span>内容</span> })
    const body = (ui.props.children as React.ReactElement).props.children
    render(body)
    expect(sessionProps.last).not.toHaveProperty("accessToken")
    expect(JSON.stringify(sessionProps.last)).not.toContain("secret-token")
    expect(sessionProps.last).toMatchObject({ user: { role: "owner" } })
  })

  it("still renders when nobody is signed in", async () => {
    authMock.mockResolvedValue(null)
    const ui = await RootLayout({ children: <span>登录</span> })
    const body = (ui.props.children as React.ReactElement).props.children
    render(body)
    expect(screen.getByText("登录")).toBeInTheDocument()
  })
})
