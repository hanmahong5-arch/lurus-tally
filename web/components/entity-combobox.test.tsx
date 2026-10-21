/**
 * Tests for EntityCombobox — search-or-create picker used by bill forms.
 */
import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { EntityCombobox, type EntityOption } from "./entity-combobox"

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const ROSE: EntityOption = { id: "p-1", name: "红叶石楠", sub: "P001" }
const PINE: EntityOption = { id: "p-2", name: "雪松", sub: "P002" }

function setup(overrides: Partial<React.ComponentProps<typeof EntityCombobox>> = {}) {
  const search = vi.fn(async (q: string) => [ROSE, PINE].filter((o) => o.name.includes(q)))
  const create = vi.fn(async (name: string): Promise<EntityOption> => ({ id: "p-new", name }))
  const onSelect = vi.fn()
  render(
    <EntityCombobox
      value=""
      label=""
      onSelect={onSelect}
      search={search}
      create={create}
      entityLabel="商品"
      debounceMs={0}
      {...overrides}
    />
  )
  return { search, create, onSelect, input: screen.getByRole("combobox") }
}

describe("EntityCombobox", () => {
  afterEach(() => vi.clearAllMocks())

  it("searches as the user types and selects an existing option", async () => {
    const { input, search, onSelect, create } = setup()
    fireEvent.change(input, { target: { value: "石楠" } })
    await waitFor(() => expect(search).toHaveBeenCalledWith("石楠", expect.anything()))
    fireEvent.mouseDown(await screen.findByText("红叶石楠"))
    expect(onSelect).toHaveBeenCalledWith(ROSE)
    expect(create).not.toHaveBeenCalled()
  })

  it("offers inline create when no option matches the name exactly, and selects the created entity", async () => {
    const { input, create, onSelect } = setup()
    fireEvent.change(input, { target: { value: "金叶女贞" } })
    const createItem = await screen.findByText(/新建商品「金叶女贞」/)
    fireEvent.mouseDown(createItem)
    await waitFor(() => expect(create).toHaveBeenCalledWith("金叶女贞"))
    await waitFor(() => expect(onSelect).toHaveBeenCalledWith({ id: "p-new", name: "金叶女贞" }))
  })

  it("does not offer create when an exact name match exists", async () => {
    const { input } = setup()
    fireEvent.change(input, { target: { value: "雪松" } })
    await screen.findByText("雪松")
    expect(screen.queryByText(/新建商品/)).toBeNull()
  })

  it("trims whitespace before creating", async () => {
    const { input, create } = setup()
    fireEvent.change(input, { target: { value: "  紫薇  " } })
    fireEvent.mouseDown(await screen.findByText(/新建商品「紫薇」/))
    await waitFor(() => expect(create).toHaveBeenCalledWith("紫薇"))
  })

  it("Enter picks the highlighted option; ArrowDown moves the highlight", async () => {
    const { input, onSelect } = setup()
    fireEvent.change(input, { target: { value: "松" } })
    await screen.findByText("雪松")
    // options: [雪松, 新建商品「松」] — highlight starts on the first
    fireEvent.keyDown(input, { key: "Enter" })
    expect(onSelect).toHaveBeenCalledWith(PINE)
  })

  it("keeps the dropdown open and shows an error when create fails", async () => {
    const create = vi.fn(async () => {
      throw new Error("boom")
    })
    const { input, onSelect } = setup({ create })
    fireEvent.change(input, { target: { value: "紫薇" } })
    fireEvent.mouseDown(await screen.findByText(/新建商品「紫薇」/))
    expect(await screen.findByText(/创建失败/)).toBeTruthy()
    expect(onSelect).not.toHaveBeenCalled()
  })

  it("renders the selected label and is read-only when disabled", () => {
    render(
      <EntityCombobox
        value="p-1"
        label="红叶石楠"
        onSelect={vi.fn()}
        search={vi.fn()}
        entityLabel="商品"
        disabled
      />
    )
    const input = screen.getByRole("combobox") as HTMLInputElement
    expect(input.value).toBe("红叶石楠")
    expect(input).toBeDisabled()
  })

  it("hides the create item when no create callback is given", async () => {
    const { input } = setup({ create: undefined })
    fireEvent.change(input, { target: { value: "金叶女贞" } })
    await waitFor(() => expect(screen.getByText(/无匹配/)).toBeTruthy())
    expect(screen.queryByText(/新建商品/)).toBeNull()
  })
})

describe("EntityCombobox refocus", () => {
  it("re-runs the search when focused again with the same query", async () => {
    const search = vi.fn(async () => [ROSE])
    render(<EntityCombobox value="" label="" onSelect={vi.fn()} search={search} entityLabel="商品" debounceMs={0} />)
    const input = screen.getByRole("combobox")
    fireEvent.focus(input)
    await screen.findByText("红叶石楠")
    fireEvent.blur(input)
    fireEvent.focus(input)
    await waitFor(() => expect(search).toHaveBeenCalledTimes(2))
    expect(await screen.findByText("红叶石楠")).toBeTruthy()
    expect(screen.queryByText("搜索中…")).toBeNull()
  })
})

describe("EntityCombobox clear", () => {
  it("calls onClear when an optional field is emptied and left", () => {
    const onClear = vi.fn()
    render(
      <EntityCombobox value="c-1" label="老张" onSelect={vi.fn()} onClear={onClear} search={vi.fn(async () => [])} entityLabel="客户" debounceMs={0} />
    )
    const input = screen.getByRole("combobox")
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: "" } })
    fireEvent.blur(input)
    expect(onClear).toHaveBeenCalledTimes(1)
  })

  it("restores the label instead of clearing when no onClear is given", () => {
    render(<EntityCombobox value="p-1" label="雪松" onSelect={vi.fn()} search={vi.fn(async () => [])} entityLabel="商品" debounceMs={0} />)
    const input = screen.getByRole("combobox") as HTMLInputElement
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: "" } })
    fireEvent.blur(input)
    expect(input.value).toBe("雪松")
  })
})
