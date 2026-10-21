import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render } from "@testing-library/react"

const signIn = vi.fn()
vi.mock("@/auth", () => ({ devProviderEnabled: () => false, signIn: (...a: unknown[]) => signIn(...a) }))

let jar: Record<string, { value: string }> = {}
vi.mock("next/headers", () => ({ cookies: () => ({ get: (n: string) => jar[n] }) }))

import LoginPage from "./page"

beforeEach(() => {
  jar = {}
})

describe("LoginPage", () => {
  it("does not show the SSO jargon", () => {
    const { container } = render(<LoginPage />)
    expect(container.textContent).not.toMatch(/SSO|单点登录/)
  })

  it("says the person joins the owner's shop after signing in when coming from an invite link", () => {
    const { container } = render(<LoginPage searchParams={{ callbackUrl: "/join?token=abc&shop=x" }} />)
    expect(container.textContent).toContain("登录后加入老板的店铺")
    expect(container.textContent).not.toContain("首次登录将引导你创建组织")
  })

  it("keeps the first-login hint for a normal sign-in", () => {
    const { container } = render(<LoginPage />)
    expect(container.textContent).not.toContain("登录后加入老板的店铺")
    expect(container.textContent).toContain("首次登录")
  })

  it("forces the account prompt when coming from a sign-out to switch person", async () => {
    signIn.mockReset()
    const { container } = render(<LoginPage searchParams={{ switch: "1" }} />)
    const form = container.querySelector("form") as HTMLFormElement
    const props = Object.keys(form).find((k) => k.startsWith("__reactProps"))
    expect(props).toBeTruthy()
    await (form as unknown as Record<string, { action: () => Promise<void> }>)[props!].action()
    expect(signIn).toHaveBeenCalledWith("oidc", { redirectTo: "/dashboard" }, { prompt: "login" })
  })

  it("does not force the prompt on a normal sign-in", async () => {
    signIn.mockReset()
    const { container } = render(<LoginPage />)
    const form = container.querySelector("form") as HTMLFormElement
    const props = Object.keys(form).find((k) => k.startsWith("__reactProps"))!
    await (form as unknown as Record<string, { action: () => Promise<void> }>)[props].action()
    expect(signIn).toHaveBeenCalledWith("oidc", { redirectTo: "/dashboard" }, undefined)
  })

  it("forces the account prompt from the leftover cookie even when the redirect dropped the query", async () => {
    signIn.mockReset()
    jar = { "tally-switch": { value: "1" } }
    const { container } = render(<LoginPage searchParams={{ callbackUrl: "/pos" }} />)
    expect(container.textContent).toContain("已退出。下一位请用自己的账号登录")
    const form = container.querySelector("form") as HTMLFormElement
    const props = Object.keys(form).find((k) => k.startsWith("__reactProps"))!
    await (form as unknown as Record<string, { action: () => Promise<void> }>)[props].action()
    expect(signIn).toHaveBeenCalledWith("oidc", { redirectTo: "/pos" }, { prompt: "login" })
  })

  it("keeps the join hint but still forces the prompt when switching via an invite link", async () => {
    signIn.mockReset()
    const { container } = render(<LoginPage searchParams={{ switch: "1", callbackUrl: "/join?token=abc" }} />)
    expect(container.textContent).toContain("登录后加入老板的店铺")
    expect(container.textContent).not.toContain("已退出。下一位")
    const form = container.querySelector("form") as HTMLFormElement
    const props = Object.keys(form).find((k) => k.startsWith("__reactProps"))!
    await (form as unknown as Record<string, { action: () => Promise<void> }>)[props].action()
    expect(signIn).toHaveBeenCalledWith("oidc", { redirectTo: "/join?token=abc" }, { prompt: "login" })
  })
  describe("微信登录按钮", () => {
    afterEach(() => {
      vi.unstubAllEnvs()
    })

    it("默认不显示", () => {
      const { container } = render(<LoginPage />)
      expect(container.textContent).not.toContain("微信登录")
    })

    it("NEXT_PUBLIC_WECHAT_LOGIN=false 时不显示", () => {
      vi.stubEnv("NEXT_PUBLIC_WECHAT_LOGIN", "false")
      const { container } = render(<LoginPage />)
      expect(container.textContent).not.toContain("微信登录")
    })

    it("为 true 时显示，点击带 idp_hint（缺省 wechat）", async () => {
      vi.stubEnv("NEXT_PUBLIC_WECHAT_LOGIN", "true")
      signIn.mockReset()
      const { container } = render(<LoginPage />)
      expect(container.textContent).toContain("微信登录")
      const forms = container.querySelectorAll("form")
      expect(forms.length).toBe(2)
      const form = forms[1] as HTMLFormElement
      const props = Object.keys(form).find((k) => k.startsWith("__reactProps"))!
      await (form as unknown as Record<string, { action: () => Promise<void> }>)[props].action()
      expect(signIn).toHaveBeenCalledWith("oidc", { redirectTo: "/dashboard" }, { idp_hint: "wechat" })
    })

    it("idp_hint 取自 NEXT_PUBLIC_WECHAT_IDP_HINT，并与切换账号的 prompt 共存", async () => {
      vi.stubEnv("NEXT_PUBLIC_WECHAT_LOGIN", "true")
      vi.stubEnv("NEXT_PUBLIC_WECHAT_IDP_HINT", "wx-open")
      signIn.mockReset()
      const { container } = render(<LoginPage searchParams={{ switch: "1" }} />)
      const form = container.querySelectorAll("form")[1] as HTMLFormElement
      const props = Object.keys(form).find((k) => k.startsWith("__reactProps"))!
      await (form as unknown as Record<string, { action: () => Promise<void> }>)[props].action()
      expect(signIn).toHaveBeenCalledWith("oidc", { redirectTo: "/dashboard" }, { prompt: "login", idp_hint: "wx-open" })
    })
  })
})
