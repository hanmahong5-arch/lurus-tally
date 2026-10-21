import { describe, it, expect } from "vitest"
import { createHash } from "node:crypto"
import { qrMatrix } from "./qr"

const URL77 = "https://tally.lurus.cn/join?token=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"

// SHA-256 of the row strings from an independent QR implementation (level L,
// same forced mask): covers versions 1, 4, 5, 6, 8, 10 (multi-block, alignment).
const REFERENCE: { text: string; mask: number; sha: string }[] = [
  { text: "hi", mask: 0, sha: "4884894b9f1a6f9f6a8135a216d0c165f87cf0c5937776ad3ccfa89226fca60b" },
  { text: URL77, mask: 3, sha: "2f5e7cebf5cf9472e62a05b3e62d2291e37b9eef6911fb6653d923f381407ea8" },
  { text: "https://tally.lurus.cn/join?token=" + "Ab9_-".repeat(12), mask: 5, sha: "03d515f41bda1650bfe6fb4ff8c5008096523453f0b43ce301d44a9db30e11b3" },
  { text: "x".repeat(120), mask: 2, sha: "ab0ca58ebf147e4a13ed57ff36ea11aee3fbbba1fd6905560dcf23544d48f896" },
  { text: "y".repeat(200), mask: 7, sha: "df32453a75e915f7d4c951520612aefe69a08678fa57f1aa1417f36db0d4179e" },
  { text: "z".repeat(250), mask: 1, sha: "dbc8e785816dd54d8872ccc7b04f79e83e98b9ec6ef366641f162889f09cdab3" },
  { text: "a".repeat(60), mask: 4, sha: "1e97b77c09dc176b067af244732dcf804e3070886ac3464c066ef5b8e3b24e70" },
]

describe("qrMatrix", () => {
  for (const r of REFERENCE) {
    it(`matches the reference for ${r.text.length} chars, mask ${r.mask}`, () => {
      const rows = qrMatrix(r.text, r.mask).map((row) => row.map((b) => (b ? "1" : "0")).join(""))
      expect(createHash("sha256").update(rows.join("")).digest("hex")).toBe(r.sha)
    })
  }
  it("picks a mask by itself and keeps the finder pattern", () => {
    const m = qrMatrix(URL77)
    expect(m.length).toBe(33)
    expect(m[0].slice(0, 7).every(Boolean)).toBe(true)
  })
  it("rejects text beyond the supported size", () => {
    expect(() => qrMatrix("x".repeat(400))).toThrow()
  })
})
