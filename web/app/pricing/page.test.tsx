import { describe, it, expect } from "vitest"
import { render } from "@testing-library/react"
import PricingPage, { metadata } from "./page"

describe("pricing page", () => {
  it("has no assistant selling points, shortcut names or unenforced limits", () => {
    const { container } = render(<PricingPage />)
    const text = container.textContent ?? ""
    expect(text).not.toMatch(/AI|智能助手|Command Palette|⌘K|Cmd|Preview|onboarding|商品数上限|用户：/)
    expect(JSON.stringify(metadata)).not.toMatch(/AI|⌘K|助手/)
  })

  it("种子期付费档只有免费试用入口，没有订阅字样", () => {
    const { container } = render(<PricingPage />)
    const text = container.textContent ?? ""
    expect(text).toContain("免费试用")
    expect(text).not.toMatch(/登录后订阅|立即订阅|购买/)
  })
})
