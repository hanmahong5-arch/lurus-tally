/**
 * Unit tests for DictionaryPage (Story 28.1).
 */
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

// Mock API module
vi.mock("@/lib/api/nursery-dict", () => ({
  listNurseryDict: vi.fn(),
  deleteNurseryDict: vi.fn(),
  restoreNurseryDict: vi.fn(),
}))

// Mock NurseryDictForm to keep tests focused on the page
vi.mock("@/components/horticulture/NurseryDictForm", () => ({
  default: () => <div>MockForm</div>,
}))

import { listNurseryDict } from "@/lib/api/nursery-dict"
import DictionaryPage from "./page"
import type { NurseryDictItem } from "@/lib/api/nursery-dict"

function makeItem(i: number): NurseryDictItem {
  return {
    id: `item-${i}`,
    tenant_id: "tenant-1",
    name: `苗木${i}`,
    latin_name: `Species ${i}`,
    family: "槭树科",
    genus: "Acer",
    type: "tree",
    is_evergreen: false,
    climate_zones: ["华东"],
    best_season: [3, 5],
    spec_template: {},
    photo_url: "",
    remark: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  }
}

describe("DictionaryPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("TestDictionaryPage_Renders_SearchInputAndTable", async () => {
    const items = [makeItem(1), makeItem(2)]
    vi.mocked(listNurseryDict).mockResolvedValueOnce({ items, total: 2 })
    render(<DictionaryPage />)
    // Search input present immediately
    expect(screen.getByPlaceholderText("搜索苗木名称...")).toBeInTheDocument()
    // After loading: table headers are visible
    await waitFor(() => {
      expect(screen.getByText("名称")).toBeInTheDocument()
    })
    expect(screen.getByText("拉丁名")).toBeInTheDocument()
    expect(screen.getByText("类型")).toBeInTheDocument()
  })

  it("TestDictionaryPage_ListItems_RenderedInTable", async () => {
    const items = [makeItem(1), makeItem(2), makeItem(3)]
    vi.mocked(listNurseryDict).mockResolvedValueOnce({ items, total: 3 })
    render(<DictionaryPage />)
    await waitFor(() => {
      expect(screen.getByText("苗木1")).toBeInTheDocument()
      expect(screen.getByText("苗木2")).toBeInTheDocument()
      expect(screen.getByText("苗木3")).toBeInTheDocument()
    })
  })

  it("TestDictionaryPage_Search_CallsApiWithQuery", async () => {
    vi.mocked(listNurseryDict).mockResolvedValue({ items: [], total: 0 })
    render(<DictionaryPage />)
    await waitFor(() => {
      expect(listNurseryDict).toHaveBeenCalledOnce()
    })
    fireEvent.change(screen.getByPlaceholderText("搜索苗木名称..."), {
      target: { value: "红枫" },
    })
    // Debounce fires after 300ms — use fake timers is complex; assert was called at least once more
    await waitFor(
      () => {
        const calls = vi.mocked(listNurseryDict).mock.calls
        const hasQuery = calls.some(
          (call) => call[0]?.q === "红枫"
        )
        expect(hasQuery).toBe(true)
      },
      { timeout: 1000 }
    )
  })

  it("TestDictionaryPage_AddButton_IsPresent", async () => {
    vi.mocked(listNurseryDict).mockResolvedValueOnce({ items: [], total: 0 })
    render(<DictionaryPage />)
    expect(screen.getByText(/新增苗木/)).toBeInTheDocument()
  })
})
