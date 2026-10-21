import { describe, it, expect } from "vitest"
import { joinStamps, owedAmount, remarkText, statusStamp } from "./print-format"
import { pageSizeRule } from "./print-shell"
import { printErrorMessage } from "./print-error"
import { ApiError, NetworkError } from "@/lib/api/errors"

describe("print helpers", () => {
  it("stamps drafts and cancelled bills only", () => {
    expect(statusStamp(0)).toBe("未审核")
    expect(statusStamp(9)).toBe("已作废")
    expect(statusStamp(2)).toBeUndefined()
    expect(joinStamps("退货", "未审核")).toBe("退货 未审核")
    expect(joinStamps(undefined, undefined)).toBeUndefined()
  })

  it("never shows a negative amount owed", () => {
    expect(owedAmount("-10.00")).toBe("0")
    expect(owedAmount("12.50")).toBe("12.5")
    expect(owedAmount(undefined)).toBe("0")
  })

  it("uses a valid @page size for till paper (explicit height, never auto)", () => {
    expect(pageSizeRule("58", 120)).toBe("58mm 120mm")
    expect(pageSizeRule("80", 90)).toBe("80mm 90mm")
    expect(pageSizeRule("a5", 90)).toBe("A5")
    expect(pageSizeRule("58", 120)).not.toContain("auto")
  })

  it("turns import remarks into a readable note", () => {
    expect(remarkText("import:shopify:1001")).toBe("网店订单 1001")
    expect(remarkText("  含运费 ")).toBe("含运费")
    expect(remarkText("")).toBeUndefined()
  })

  it("maps backend errors to Chinese without leaking English", () => {
    expect(printErrorMessage(new ApiError(404, "not_found", "sale bill not found", { detail: "sale bill not found" }))).toMatch(/找不到/)
    expect(printErrorMessage(new ApiError(400, "bad", "invalid bill id"))).toMatch(/找不到/)
    expect(printErrorMessage(new ApiError(500, "x", "an internal error occurred"))).not.toMatch(/[a-z]{4}/)
    expect(printErrorMessage(new NetworkError("offline", "x"))).toMatch(/网络/)
  })
})
