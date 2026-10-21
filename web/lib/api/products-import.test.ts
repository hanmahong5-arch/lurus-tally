import { describe, it, expect, vi, afterEach } from "vitest"
import {
  CSV_BOM,
  buildImportProblemCsv,
  importProducts,
  readCsvAsText,
  type ProductImportResult,
} from "./products"
import { extractApiError } from "./errors"

// GBK bytes for "名称,编码\n螺丝,L1\n" (what Excel writes for a Chinese CSV).
const GBK_BYTES = new Uint8Array([
  0xc3, 0xfb, 0xb3, 0xc6, 0x2c, 0xb1, 0xe0, 0xc2, 0xeb, 0x0a, 0xc2, 0xdd, 0xcb, 0xbf, 0x2c, 0x4c, 0x31, 0x0a,
])

function fileOf(bytes: Uint8Array | string, name = "p.csv"): File {
  const part = typeof bytes === "string" ? bytes : new Blob([bytes as BlobPart])
  return new File([part], name, { type: "text/csv" })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("readCsvAsText", () => {
  it("reads UTF-8 as is and drops the BOM", async () => {
    expect(await readCsvAsText(fileOf(CSV_BOM + "名称\n螺丝\n"))).toBe("名称\n螺丝\n")
  })

  it("falls back to GBK for files Excel saved in Chinese encoding", async () => {
    expect(await readCsvAsText(fileOf(GBK_BYTES))).toBe("名称,编码\n螺丝,L1\n")
  })

  it("refuses Excel workbooks with a plain-Chinese hint", async () => {
    await expect(readCsvAsText(fileOf("x", "商品.xlsx"))).rejects.toThrow("另存为")
  })
})

describe("importProducts", () => {
  it("posts the file as UTF-8 text and passes the dry-run flag", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ dry_run: true, total: 1, valid: 1, invalid: 0, imported: 0, rows: [] }), {
        status: 200,
      })
    )
    vi.stubGlobal("fetch", fetchMock)
    const res = await importProducts(fileOf(GBK_BYTES), { dryRun: true, tenantId: "t1" })
    expect(res.valid).toBe(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe("/api/proxy/products/import?dry_run=true")
    const sent = (init.body as FormData).get("file") as File
    expect(await sent.text()).toBe("名称,编码\n螺丝,L1\n")
    expect((init.headers as Record<string, string>)["Idempotency-Key"]).toBeTruthy()
  })

  it("commit call has no dry_run flag", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }))
    vi.stubGlobal("fetch", fetchMock)
    await importProducts(fileOf("名称\na\n"), { dryRun: false })
    expect((fetchMock.mock.calls[0] as [string])[0]).toBe("/api/proxy/products/import")
  })

  it("shows the server's Chinese message and hides technical ones", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "表格里没有商品数据" }), { status: 400 }))
    )
    const err = await importProducts(fileOf("x"), { dryRun: true }).catch((e) => e)
    expect(extractApiError(err).message).toBe("表格里没有商品数据")

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "bad request body" }), { status: 400 }))
    )
    const err2 = await importProducts(fileOf("x"), { dryRun: true }).catch((e) => e)
    expect(extractApiError(err2).message).toBe("导入失败，请检查文件后重试")

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("boom", { status: 500 })))
    const err3 = await importProducts(fileOf("x"), { dryRun: true }).catch((e) => e)
    expect(extractApiError(err3).message).toBe("服务暂时不可用，请稍后重试")
  })
})

describe("buildImportProblemCsv", () => {
  it("lists only failed rows, quoting commas, with a BOM for Excel", () => {
    const result: ProductImportResult = {
      dry_run: true,
      total: 2,
      valid: 1,
      invalid: 1,
      imported: 0,
      rows: [
        { line: 2, name: "好商品", status: "ok", imported: false },
        { line: 3, name: "螺丝,M4", code: "L1", status: "error", errors: ["编码「L1」已经有商品在用了", "零售价不能为负数"], imported: false },
      ],
    }
    const csv = buildImportProblemCsv(result)
    expect(csv.startsWith(CSV_BOM + "行号,名称,编码,问题\n")).toBe(true)
    expect(csv).toContain('3,"螺丝,M4",L1,编码「L1」已经有商品在用了；零售价不能为负数')
    expect(csv).not.toContain("好商品")
  })
})
