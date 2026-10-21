import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"

const { authMock } = vi.hoisted(() => ({ authMock: vi.fn() }))
vi.mock("@/auth", () => ({ auth: authMock }))

import SubscriptionLayout from "./layout"

async function renderLayout() {
  const ui = await SubscriptionLayout({ children: <p>套餐列表</p> })
  render(ui)
}

describe("SubscriptionLayout", () => {
  beforeEach(() => authMock.mockReset())

  it("shows the plans to the owner", async () => {
    authMock.mockResolvedValue({ user: { role: "owner", isOwner: true } })
    await renderLayout()
    expect(screen.getByText("套餐列表")).toBeInTheDocument()
  })

  it("does not block while the role is unknown", async () => {
    authMock.mockResolvedValue(null)
    await renderLayout()
    expect(screen.getByText("套餐列表")).toBeInTheDocument()
  })

  it.each(["manager", "clerk", "cashier", "finance"])("blocks %s", async (role) => {
    authMock.mockResolvedValue({ user: { role, isOwner: false } })
    await renderLayout()
    expect(screen.queryByText("套餐列表")).not.toBeInTheDocument()
    expect(screen.getByText("套餐和付费只有老板能看")).toBeInTheDocument()
  })
})
