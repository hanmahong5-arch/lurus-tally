import { describe, it, expect, vi } from "vitest"
import { render } from "@testing-library/react"

vi.mock("@/auth", () => ({ auth: vi.fn().mockResolvedValue({ accessToken: "tok" }) }))
vi.mock("next/navigation", () => ({ redirect: vi.fn() }))
vi.mock("@/lib/api/me", () => ({
  getMe: vi.fn().mockResolvedValue({ profile_type: "" }),
  chooseProfile: vi.fn(),
  TallyApiError: class extends Error {},
}))
vi.mock("@/components/onboarding/OnboardingWizard", () => ({ default: () => null }))

import SetupPage from "./page"

describe("SetupPage", () => {
  it("explains the three business types without accounting or system jargon", async () => {
    const { container } = render(await SetupPage({}))
    expect(container.textContent).not.toMatch(/WAC|FIFO|hybrid|HS Code|SKU|POS/)
    expect(container.textContent).toContain("零售 / 批发")
  })

  it("promises only what the nursery screens have", async () => {
    const { container } = render(await SetupPage({}))
    expect(container.textContent).toContain("苗木字典")
    expect(container.textContent).toContain("送货单带规格")
    expect(container.textContent).toContain("按株 / 棵 / 平方米计价")
    expect(container.textContent).not.toMatch(/200\+|价格分级|损益核算|拍照存档/)
  })

  it("never prints a raw error code", async () => {
    const { container } = render(await SetupPage({ searchParams: { error: "api_error: pq 23505" } }))
    expect(container.textContent).not.toContain("23505")
    expect(container.textContent).toContain("保存没成功")
  })

  it("shows the server's Chinese explanation as is", async () => {
    const { container } = render(await SetupPage({ searchParams: { error: "该账号已选择过业务类型" } }))
    expect(container.textContent).toContain("该账号已选择过业务类型")
  })

  it("tells invited staff to open the owner's invite link instead of opening a shop", async () => {
    const { container } = render(await SetupPage({}))
    expect(container.textContent).toContain("如果是老板邀请你，请打开老板发的邀请链接")
  })
})
