import { describe, it, expect } from "vitest"
import { moneyToChinese } from "./money-cn"

describe("moneyToChinese", () => {
  const cases: [string | number, string][] = [
    ["0", "零元整"],
    ["0.00", "零元整"],
    ["1", "壹元整"],
    ["10", "壹拾元整"],
    ["105", "壹佰零伍元整"],
    ["1000", "壹仟元整"],
    ["1010", "壹仟零壹拾元整"],
    ["10000", "壹万元整"],
    ["10005", "壹万零伍元整"],
    ["100000", "壹拾万元整"],
    ["100000001", "壹亿零壹元整"],
    ["1234567.89", "壹佰贰拾叁万肆仟伍佰陆拾柒元捌角玖分"],
    ["0.5", "伍角"],
    ["0.05", "伍分"],
    ["1.5", "壹元伍角"],
    ["1.05", "壹元零伍分"],
    ["1.55", "壹元伍角伍分"],
    ["20000.1", "贰万元壹角"],
    ["120000", "壹拾贰万元整"],
    ["100010000", "壹亿零壹万元整"],
    ["99.999", "壹佰元整"],
    ["-12.3", "负壹拾贰元叁角"],
    [88, "捌拾捌元整"],
  ]
  for (const [input, want] of cases) {
    it(`${input} -> ${want}`, () => {
      expect(moneyToChinese(input)).toBe(want)
    })
  }

  it("returns empty string for non-numeric input", () => {
    expect(moneyToChinese("abc")).toBe("")
    expect(moneyToChinese(null)).toBe("")
    expect(moneyToChinese("")).toBe("")
  })

  it("returns empty string beyond the supported range", () => {
    expect(moneyToChinese("1000000000000000000")).toBe("")
  })
})
