import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import * as api from "@/lib/api/units"
import { ApiError } from "@/lib/api/errors"
import { UnitSelector } from "./unit-selector"

vi.mock("@/lib/api/units", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/units")>("@/lib/api/units")
  return { ...actual, listUnits: vi.fn(), createUnit: vi.fn() }
})

const unit = (id: string, name: string, code: string) =>
  ({ id, name, code, unit_type: "count", is_system: false }) as unknown as api.UnitDef

describe("UnitSelector", () => {
  beforeEach(() => {
    vi.mocked(api.listUnits).mockReset()
    vi.mocked(api.createUnit).mockReset()
  })

  it("lists the tenant's units including the industry default 株", async () => {
    vi.mocked(api.listUnits).mockResolvedValue({ items: [unit("u-1", "株", "zhu")] })
    render(<UnitSelector onChange={() => {}} />)
    expect(await screen.findByText(/株/)).toBeTruthy()
    expect(screen.getByText("+ 新建单位")).toBeTruthy()
  })

  it("shows a plain-Chinese message, not a stringified error, when loading fails", async () => {
    vi.mocked(api.listUnits).mockRejectedValue(new Error("boom"))
    render(<UnitSelector onChange={() => {}} />)
    const opt = await screen.findByText(/单位没加载出来/)
    expect(opt.textContent).not.toMatch(/Error:|boom/)
  })

  it("creates a unit inline, selects it, and surfaces create errors in Chinese", async () => {
    vi.mocked(api.listUnits).mockResolvedValue({ items: [unit("u-1", "株", "zhu")] })
    const onChange = vi.fn()
    render(<UnitSelector onChange={onChange} />)
    await screen.findByText(/株/)

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "__new__" } })
    const input = screen.getByPlaceholderText("单位名称，如：盆")

    vi.mocked(api.createUnit).mockRejectedValueOnce(
      new ApiError(409, "conflict", "x", { detail: "这个单位已经有了" }),
    )
    fireEvent.change(input, { target: { value: "盆" } })
    fireEvent.click(screen.getByText("添加"))
    expect(await screen.findByText("这个单位已经有了")).toBeTruthy()

    vi.mocked(api.createUnit).mockResolvedValueOnce(unit("u-2", "盆", "pen"))
    fireEvent.click(screen.getByText("添加"))
    await waitFor(() => expect(onChange).toHaveBeenCalledWith("u-2"))
    expect(api.createUnit).toHaveBeenLastCalledWith({ name: "盆", unit_type: "count" }, undefined)
  })
  it("pressing Enter in the new-unit box adds the unit and does not submit the surrounding form", async () => {
    vi.mocked(api.listUnits).mockResolvedValue({ items: [unit("u-1", "株", "zhu")] })
    vi.mocked(api.createUnit).mockResolvedValue(unit("u-2", "盆", "pen"))
    const onChange = vi.fn()
    const submit = vi.fn((e: React.FormEvent) => e.preventDefault())
    render(
      <form onSubmit={submit}>
        <UnitSelector onChange={onChange} />
      </form>,
    )
    await screen.findByText(/株/)
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "__new__" } })
    const input = screen.getByPlaceholderText("单位名称，如：盆")
    fireEvent.change(input, { target: { value: "盆" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(api.createUnit).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(onChange).toHaveBeenCalledWith("u-2"))
    expect(submit).not.toHaveBeenCalled()
  })

  it("lets the owner pick a measuring kind for a new unit, and Escape cancels", async () => {
    vi.mocked(api.listUnits).mockResolvedValue({ items: [] })
    vi.mocked(api.createUnit).mockResolvedValue(unit("u-3", "亩", "mu"))
    render(<UnitSelector onChange={() => {}} />)
    await screen.findByText("+ 新建单位")
    fireEvent.change(screen.getAllByRole("combobox")[0], { target: { value: "__new__" } })
    fireEvent.change(screen.getByLabelText("计量方式"), { target: { value: "area" } })
    const input = screen.getByPlaceholderText("单位名称，如：盆")
    fireEvent.change(input, { target: { value: "亩" } })
    fireEvent.click(screen.getByText("添加"))
    await waitFor(() =>
      expect(api.createUnit).toHaveBeenCalledWith({ name: "亩", unit_type: "area" }, undefined),
    )
  })

  it("shows only unit names, shop-made units first", async () => {
    vi.mocked(api.listUnits).mockResolvedValue({
      items: [
        { ...unit("s-1", "件", "pcs"), is_system: true },
        unit("u-1", "株", "zhu"),
      ],
    })
    render(<UnitSelector onChange={() => {}} />)
    await screen.findByText("株")
    const texts = screen.getAllByRole("option").map((o) => o.textContent)
    expect(texts.slice(1, 3)).toEqual(["株", "件"])
    expect(texts.join("")).not.toMatch(/zhu|pcs|系统/)
  })
})

describe("UnitSelector for a nursery", () => {
  it("lists 株 first and selects the existing one instead of adding a second", async () => {
    vi.resetModules()
    vi.doMock("@/lib/profile", () => ({ useProfile: () => ({ profileType: "horticulture" }) }))
    const { UnitSelector: Nursery } = await import("./unit-selector")
    const apiMod = await import("@/lib/api/units")
    vi.mocked(apiMod.listUnits).mockResolvedValue({
      items: [unit("u-2", "盆", "pen"), unit("u-3", "平方米", "m2"), unit("u-1", "株", "zhu")],
    })
    const onChange = vi.fn()
    render(<Nursery onChange={onChange} />)
    await screen.findByText("株")
    const names = screen.getAllByRole("option").map((o) => o.textContent)
    expect(names.slice(1, 4)).toEqual(["株", "平方米", "盆"])

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "__new__" } })
    fireEvent.change(screen.getByPlaceholderText("单位名称，如：盆"), { target: { value: "株" } })
    fireEvent.click(screen.getByText("添加"))
    expect(onChange).toHaveBeenCalledWith("u-1")
    expect(apiMod.createUnit).not.toHaveBeenCalled()
    expect(await screen.findByText("已有这个单位，已帮你选上")).toBeTruthy()
    vi.doUnmock("@/lib/profile")
  })

  it("treats full-width and upper-case spellings as the same unit", async () => {
    vi.resetModules()
    vi.doMock("@/lib/profile", () => ({ useProfile: () => ({ profileType: "horticulture" }) }))
    const { UnitSelector: Nursery } = await import("./unit-selector")
    const apiMod = await import("@/lib/api/units")
    vi.mocked(apiMod.listUnits).mockResolvedValue({ items: [unit("u-7", "kg", "kg")] })
    vi.mocked(apiMod.createUnit).mockClear()
    const onChange = vi.fn()
    render(<Nursery onChange={onChange} />)
    await screen.findByText("kg")
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "__new__" } })
    fireEvent.change(screen.getByPlaceholderText("单位名称，如：盆"), { target: { value: "ＫＧ" } })
    fireEvent.click(screen.getByText("添加"))
    expect(onChange).toHaveBeenCalledWith("u-7")
    expect(apiMod.createUnit).not.toHaveBeenCalled()
    vi.doUnmock("@/lib/profile")
  })
})
