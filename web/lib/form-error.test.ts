import { describe, it, expect } from "vitest"
import { ApiError } from "@/lib/api/errors"
import { formSaveError } from "./form-error"

describe("formSaveError", () => {
  it("shows the conflict copy on 409", () => {
    const e = new ApiError(409, "conflict", "HTTP 409", { error: "duplicate code" })
    expect(formSaveError(e, "重复了")).toBe("重复了")
  })

  it("replaces English 4xx text", () => {
    const e = new ApiError(400, "bad", "HTTP 400", { error: "project create: ERROR: invalid input (SQLSTATE 22P02)" })
    expect(formSaveError(e, "重复了")).toBe("保存没成功，请检查填写的内容")
  })

  it("keeps a Chinese server message", () => {
    const e = new ApiError(400, "bad", "HTTP 400", { message: "完工日期不能早于开工日期" })
    expect(formSaveError(e, "重复了")).toBe("完工日期不能早于开工日期")
  })

  it("lets 5xx and network errors through extractApiError", () => {
    expect(formSaveError(new ApiError(500, "x", "boom"), "c")).toContain("系统出了点问题")
  })
})
