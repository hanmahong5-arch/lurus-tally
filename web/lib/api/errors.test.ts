import { describe, it, expect } from "vitest"
import { ApiError, NetworkError, extractApiError } from "./errors"

const FRIENDLY_5XX = "系统出了点问题，请稍后再试；一直不行请联系客服"

describe("extractApiError", () => {
  it("reads the server action from the body", () => {
    const e = new ApiError(409, "demo_in_use", "x", { message: "示例数据没清掉", action: "去商品页停用" })
    const d = extractApiError(e)
    expect(d.message).toBe("示例数据没清掉")
    expect(d.action).toBe("去商品页停用")
  })

  it("leaves action undefined when the body has none", () => {
    expect(extractApiError(new ApiError(400, "bad", "参数不对")).action).toBeUndefined()
  })

  it("replaces an English 5xx message with Chinese copy", () => {
    const e = new ApiError(500, "internal_error", "an internal error occurred", {
      message: "an internal error occurred",
      action: "retry shortly",
    })
    const d = extractApiError(e)
    expect(d.message).toBe(FRIENDLY_5XX)
    expect(d.action).toBeUndefined()
  })

  it("keeps a Chinese 5xx message", () => {
    const e = new ApiError(500, "clear_demo_failed", "x", { message: "清除示例数据没有成功", action: "请稍后再试" })
    const d = extractApiError(e)
    expect(d.message).toBe("清除示例数据没有成功")
    expect(d.action).toBe("请稍后再试")
  })

  it("keeps an English 4xx message untouched", () => {
    expect(extractApiError(new ApiError(404, "nf", "not found")).message).toBe("not found")
  })

  it("maps network errors and unknown values to Chinese", () => {
    expect(extractApiError(new NetworkError("offline", "x")).message).toContain("网络")
    expect(extractApiError("boom").message).toContain("操作失败")
  })
})
