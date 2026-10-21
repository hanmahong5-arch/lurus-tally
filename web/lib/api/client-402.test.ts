/**
 * 402 subscription_expired: toast once per page load, never the network-error toast.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"

const toastError = vi.fn()
vi.mock("sonner", () => ({ toast: { error: toastError } }))
const { state } = vi.hoisted(() => ({ state: { role: "owner" as string } }))
vi.mock("next-auth/react", () => ({
  getSession: async () => ({ user: { role: state.role } }),
  signIn: vi.fn(),
}))

function stub402(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 402 }))),
  )
}

const EXPIRED = { code: "subscription_expired", message: "套餐已到期，现在只能查看和导出，续费后恢复开单。", plan_code: "pro" }

describe("apiFetch 402 subscription_expired", () => {
  beforeEach(() => {
    vi.resetModules()
    state.role = "owner"
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    toastError.mockClear()
  })

  it("toasts once for repeated 402s, with the subscribe action for the owner", async () => {
    stub402(EXPIRED)
    const { apiFetch } = await import("./client")
    const { ApiError } = await import("./errors")
    const e1 = await apiFetch("/sale-bills", { method: "POST" }).catch((e) => e)
    await apiFetch("/sale-bills", { method: "POST" }).catch(() => undefined)
    expect(e1).toBeInstanceOf(ApiError)
    expect((e1 as InstanceType<typeof ApiError>).status).toBe(402)
    expect(toastError).toHaveBeenCalledTimes(1)
    expect(toastError.mock.calls[0][0]).toBe("套餐已到期，现在只能查看和导出")
    expect(toastError.mock.calls[0][1].action.label).toBe("去订阅页")
    for (const call of toastError.mock.calls) expect(call[0]).not.toContain("网络异常")
  })

  it("tells a non-owner to contact the owner, without an action", async () => {
    state.role = "clerk"
    stub402(EXPIRED)
    const { apiFetch } = await import("./client")
    await apiFetch("/sale-bills", { method: "POST" }).catch(() => undefined)
    expect(toastError).toHaveBeenCalledTimes(1)
    expect(toastError.mock.calls[0][0]).toContain("请联系老板续费")
    expect(toastError.mock.calls[0][1]?.action).toBeUndefined()
  })

  it("stays quiet when silent, and ignores other 402s", async () => {
    stub402(EXPIRED)
    const { apiFetch } = await import("./client")
    await apiFetch("/x", { method: "POST", silent: true }).catch(() => undefined)
    stub402({ error: "insufficient_balance" })
    await apiFetch("/y", { method: "POST" }).catch(() => undefined)
    expect(toastError).not.toHaveBeenCalled()
  })
})
