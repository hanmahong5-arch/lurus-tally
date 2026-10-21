import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { CsvImportDialog } from "./csv-import-dialog"

const result = {
  created: 2,
  skipped: 1,
  errors: [{ row: 5, message: "名称不能为空" }],
}

function setup(over: Partial<React.ComponentProps<typeof CsvImportDialog>> = {}) {
  const props = {
    open: true,
    onOpenChange: vi.fn(),
    noun: "客户",
    templateFilename: "客户导入模板.csv",
    fetchTemplate: vi.fn().mockResolvedValue("客户名称*,联系人\n"),
    importFile: vi.fn().mockResolvedValue(result),
    onImported: vi.fn(),
    ...over,
  }
  render(<CsvImportDialog {...props} />)
  return props
}

function pick(text: string) {
  const input = screen.getByLabelText("选择客户表格文件") as HTMLInputElement
  fireEvent.change(input, { target: { files: [new File([text], "客户.csv", { type: "text/csv" })] } })
}

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => "blob:t")
  URL.revokeObjectURL = vi.fn()
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {})
})

describe("CsvImportDialog", () => {
  it("step 1: downloads the template through the given fetcher", async () => {
    const p = setup()
    fireEvent.click(screen.getByRole("button", { name: "下载模板" }))
    await waitFor(() => expect(p.fetchTemplate).toHaveBeenCalled())
    expect(URL.createObjectURL).toHaveBeenCalled()
  })

  it("step 2: previews at most 20 rows, then step 3 shows the summary and failed rows", async () => {
    const lines = ["名称,联系人"]
    for (let i = 1; i <= 25; i++) lines.push(`客户${i},联系${i}`)
    const p = setup()
    pick(lines.join("\n") + "\n")
    await screen.findByText("客户20")
    expect(screen.queryByText("客户21")).toBeNull()
    expect(screen.getByText(/只显示前 20 行/)).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "确认导入" }))
    await screen.findByTestId("import-summary")
    expect(screen.getByTestId("import-summary").textContent).toBe("新增 2、跳过 1、失败 1。")
    expect(screen.getByText(/第 5 行/)).toBeTruthy()
    expect(screen.getByText(/名称不能为空/)).toBeTruthy()
    expect(p.importFile).toHaveBeenCalledTimes(1)
    expect(p.onImported).toHaveBeenCalled()
  })

  it("splits skipped rows into name and phone duplicates", async () => {
    setup({
      importFile: vi.fn().mockResolvedValue({
        created: 1,
        skipped: 3,
        skipped_by_name: 2,
        skipped_by_phone: 1,
        skipped_rows: [{ row: 4, message: "手机号重复，已跳过" }],
        errors: [],
      }),
    })
    pick("名称\n甲\n")
    fireEvent.click(await screen.findByRole("button", { name: "确认导入" }))
    const note = await screen.findByTestId("import-skipped")
    expect(note.textContent).toContain("2 行因名称重复")
    expect(note.textContent).toContain("1 行因手机号重复")
    expect(screen.getByTestId("import-skipped-rows").textContent).toContain("手机号重复")
  })

  it("does not reload the list when nothing was created", async () => {
    const p = setup({ importFile: vi.fn().mockResolvedValue({ created: 0, skipped: 3, errors: [] }) })
    pick("名称\n甲\n")
    fireEvent.click(await screen.findByRole("button", { name: "确认导入" }))
    await screen.findByTestId("import-summary")
    expect(p.onImported).not.toHaveBeenCalled()
  })

  it("stays on the preview with a banner when the import fails", async () => {
    const p = setup({ importFile: vi.fn().mockRejectedValue(new Error("boom")) })
    pick("名称\n甲\n")
    fireEvent.click(await screen.findByRole("button", { name: "确认导入" }))
    await waitFor(() => expect(p.importFile).toHaveBeenCalled())
    expect(await screen.findByRole("button", { name: "确认导入" })).toBeTruthy()
    expect(screen.queryByTestId("import-summary")).toBeNull()
  })
})
