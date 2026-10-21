import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

vi.mock("@/lib/api/products", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/products")>("@/lib/api/products")
  return { ...actual, importProducts: vi.fn() }
})

const { roleState } = vi.hoisted(() => ({ roleState: { role: "owner" as string } }))
vi.mock("next-auth/react", () => ({ useSession: () => ({ data: { user: { role: roleState.role } } }) }))

import { importProducts, type ProductImportResult } from "@/lib/api/products"
import { ApiError } from "@/lib/api/errors"
import { ProductImportDialog } from "./product-import-dialog"

const dry: ProductImportResult = {
  dry_run: true,
  total: 3,
  valid: 2,
  invalid: 1,
  imported: 0,
  created_units: ["盒"],
  rows: [
    { line: 2, name: "螺丝", status: "ok", imported: false },
    { line: 3, name: "水管", status: "ok", imported: false },
    { line: 4, name: "坏行", status: "error", errors: ["编码「L1」已经有商品在用了"], imported: false },
  ],
}

function pick(file: File) {
  const input = screen.getByLabelText("选择商品表格文件") as HTMLInputElement
  fireEvent.change(input, { target: { files: [file] } })
}

const csv = () => new File(["名称\n螺丝\n"], "商品.csv", { type: "text/csv" })

beforeEach(() => {
  vi.mocked(importProducts).mockReset()
  roleState.role = "owner"
})

/** Clicks "下载模板" and returns the CSV text the browser would have saved. */
async function downloadedTemplate(): Promise<string> {
  let blob: Blob | null = null
  URL.createObjectURL = vi.fn((b: Blob | MediaSource) => {
    blob = b as Blob
    return "blob:t"
  })
  URL.revokeObjectURL = vi.fn()
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {})
  render(<ProductImportDialog open onOpenChange={vi.fn()} onImported={vi.fn()} />)
  fireEvent.click(screen.getByRole("button", { name: "下载模板" }))
  expect(blob).not.toBeNull()
  return await new Promise<string>((resolve) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result))
    r.readAsText(blob as unknown as Blob)
  })
}

describe("ProductImportDialog", () => {
  it("checks first, lists problems in words, then imports only after confirmation", async () => {
    vi.mocked(importProducts).mockResolvedValueOnce(dry)
    const onImported = vi.fn()
    render(<ProductImportDialog open onOpenChange={vi.fn()} onImported={onImported} />)
    pick(csv())

    expect(await screen.findByText(/2 行可以导入/)).toBeTruthy()
    expect(screen.getByText(/1 行有问题/)).toBeTruthy()
    expect(screen.getByText(/编码「L1」已经有商品在用了/)).toBeTruthy()
    expect(screen.getByText(/自动新建这些单位：盒/)).toBeTruthy()
    expect(vi.mocked(importProducts).mock.calls[0][1]).toMatchObject({ dryRun: true })
    expect(onImported).not.toHaveBeenCalled()

    vi.mocked(importProducts).mockResolvedValueOnce({ ...dry, dry_run: false, imported: 2 })
    fireEvent.click(screen.getByRole("button", { name: "确认导入 2 个商品" }))
    expect(await screen.findByText(/已导入/)).toBeTruthy()
    expect(vi.mocked(importProducts).mock.calls[1][1]).toMatchObject({ dryRun: false })
    expect(onImported).toHaveBeenCalledTimes(1)
  })

  it("cannot confirm when no row is valid", async () => {
    vi.mocked(importProducts).mockResolvedValueOnce({ ...dry, valid: 0, invalid: 3 })
    render(<ProductImportDialog open onOpenChange={vi.fn()} onImported={vi.fn()} />)
    pick(csv())
    const btn = (await screen.findByRole("button", { name: "没有可导入的商品" })) as HTMLButtonElement
    expect(btn.disabled).toBe(true)
  })

  it("shows the Chinese reason when the file is rejected and stays on the picker", async () => {
    vi.mocked(importProducts).mockRejectedValueOnce(new ApiError(400, "import_failed", "表格里没有商品数据", {}))
    render(<ProductImportDialog open onOpenChange={vi.fn()} onImported={vi.fn()} />)
    pick(csv())
    expect(await screen.findByText("表格里没有商品数据")).toBeTruthy()
    await waitFor(() => expect(screen.getByRole("button", { name: "选择文件" })).toBeTruthy())
  })

  it("gives the boss a template with the purchase price columns", async () => {
    const text = await downloadedTemplate()
    expect(text).toContain("进价")
    expect(text).toContain("期初成本")
  })

  it("gives a clerk a template without purchase price or opening cost", async () => {
    roleState.role = "clerk"
    const text = await downloadedTemplate()
    expect(text).not.toContain("进价")
    expect(text).not.toContain("期初成本")
    expect(text).toContain("期初数量")
    const [header, row] = text.replace(/^\uFEFF/, "").split("\n")
    expect(header.split(",")).toHaveLength(9)
    expect(row.split(",").length).toBeGreaterThanOrEqual(9)
  })
})
