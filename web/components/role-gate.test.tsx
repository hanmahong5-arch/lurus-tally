import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, act } from "@testing-library/react"

const { state } = vi.hoisted(() => ({ state: { role: null as string | null } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: state.role ? { role: state.role } : {} } }),
}))

import { RoleGate } from "./role-gate"

describe("RoleGate waiting for the role", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    state.role = null
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it("shows loading, then suggests a refresh after 8 seconds", () => {
    render(
      <RoleGate perm="payment.read" title="收付款" notice="没有权限" waitForRole>
        <p>页面内容</p>
      </RoleGate>,
    )
    expect(screen.getByText("加载中...")).toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(7999)
    })
    expect(screen.getByText("加载中...")).toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(screen.getByText("暂时无法确认你的岗位，请刷新页面重试。")).toBeInTheDocument()
    expect(screen.queryByText("页面内容")).toBeNull()
  })

  it("does not wait when the page asks for nothing restricted", () => {
    render(
      <RoleGate perm="payment.read" title="收付款" notice="没有权限">
        <p>页面内容</p>
      </RoleGate>,
    )
    expect(screen.getByText("页面内容")).toBeInTheDocument()
  })

  it("shows the note to a role without the permission once known", () => {
    state.role = "cashier"
    render(
      <RoleGate perm="payment.read" title="收付款" notice="收付款只对老板、店长和财务开放" waitForRole>
        <p>页面内容</p>
      </RoleGate>,
    )
    expect(screen.getByText("收付款只对老板、店长和财务开放")).toBeInTheDocument()
    expect(screen.queryByText("页面内容")).toBeNull()
  })
})
