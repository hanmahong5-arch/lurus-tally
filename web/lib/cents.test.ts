import { describe, it, expect } from "vitest"
import { roundCents, lineCents } from "./cents"

describe("cents", () => {
  it("rounds half away from zero to the cent", () => {
    expect(roundCents(41.625)).toBe(41.63)
    expect(roundCents(1.005)).toBe(1.01)
    expect(roundCents(4.995)).toBe(5)
    expect(roundCents(-41.625)).toBe(-41.63)
  })

  it("prices a line to the cent", () => {
    expect(lineCents(12.5, 3.33)).toBe(41.63)
    expect(lineCents(1.5, 3.33)).toBe(5)
  })
})
