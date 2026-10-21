/**
 * 403 toast: show the backend's Chinese message when it gives one, else the
 * generic "权限不足".
 */
import { describe, it, expect, vi, afterEach } from "vitest"

const toastError = vi.fn()
vi.mock("sonner", () => ({ toast: { error: toastError } }))

import { apiFetch } from "./client"
import { ApiError } from "./errors"

function stub403(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 403 }))),
  )
}

describe("apiFetch 403 handling", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    toastError.mockClear()
  })

  it("shows the backend Chinese message", async () => {
    stub403({ error: "forbidden", message: "收银员不能修改仓库，请联系店主" })
    const err = await apiFetch("/warehouses", { method: "POST" }).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(toastError).toHaveBeenCalledWith("收银员不能修改仓库，请联系店主")
  })

  it("prefers detail over message", async () => {
    stub403({ detail: "财务不能新增苗木", message: "forbidden" })
    await apiFetch("/x", { method: "POST" }).catch(() => undefined)
    expect(toastError).toHaveBeenCalledWith("财务不能新增苗木")
  })

  it("falls back to 权限不足 when the message is English or missing", async () => {
    stub403({ error: "forbidden", message: "forbidden" })
    await apiFetch("/x", { method: "POST" }).catch(() => undefined)
    expect(toastError).toHaveBeenLastCalledWith("权限不足")
    stub403({})
    await apiFetch("/x", { method: "POST" }).catch(() => undefined)
    expect(toastError).toHaveBeenLastCalledWith("权限不足")
  })

  it("does not toast when silent", async () => {
    stub403({ message: "没有权限" })
    await apiFetch("/x", { method: "POST", silent: true }).catch(() => undefined)
    expect(toastError).not.toHaveBeenCalled()
  })
})
