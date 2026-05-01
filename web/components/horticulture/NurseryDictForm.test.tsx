/**
 * Unit tests for NurseryDictForm component (Story 28.1).
 */
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

// Mock the API
vi.mock("@/lib/api/nursery-dict", () => ({
  createNurseryDict: vi.fn(),
  updateNurseryDict: vi.fn(),
}))

import { createNurseryDict, updateNurseryDict } from "@/lib/api/nursery-dict"
import NurseryDictForm from "./NurseryDictForm"
import type { NurseryDictItem } from "@/lib/api/nursery-dict"

const mockItem: NurseryDictItem = {
  id: "item-1",
  tenant_id: "tenant-1",
  name: "红枫",
  latin_name: "Acer palmatum",
  family: "槭树科",
  genus: "Acer",
  type: "tree",
  is_evergreen: false,
  climate_zones: ["华东"],
  best_season: [3, 5],
  spec_template: { 胸径_cm: null },
  photo_url: "",
  remark: "",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
}

describe("NurseryDictForm", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("TestNurseryDictForm_RequiredFields_ShowsValidationError", async () => {
    const onSuccess = vi.fn()
    const onCancel = vi.fn()
    render(
      <NurseryDictForm mode="create" onSuccess={onSuccess} onCancel={onCancel} />
    )

    // Submit with empty name
    fireEvent.click(screen.getByRole("button", { name: /创建/ }))

    await waitFor(() => {
      expect(screen.getByText("名称不能为空")).toBeInTheDocument()
    })
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it("TestNurseryDictForm_SubmitCreate_CallsCreateApi", async () => {
    vi.mocked(createNurseryDict).mockResolvedValueOnce({
      ...mockItem,
      name: "测试苗",
    })
    const onSuccess = vi.fn()
    const onCancel = vi.fn()
    render(
      <NurseryDictForm mode="create" onSuccess={onSuccess} onCancel={onCancel} />
    )

    fireEvent.change(screen.getByPlaceholderText("请输入苗木名称"), {
      target: { value: "测试苗" },
    })
    fireEvent.click(screen.getByRole("button", { name: /创建/ }))

    await waitFor(() => {
      expect(createNurseryDict).toHaveBeenCalledOnce()
    })
    const callArg = vi.mocked(createNurseryDict).mock.calls[0][0]
    expect(callArg.name).toBe("测试苗")
    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith(expect.objectContaining({ name: "测试苗" }))
    })
  })

  it("TestNurseryDictForm_SubmitEdit_CallsUpdateApi", async () => {
    const updatedItem = { ...mockItem, remark: "已更新" }
    vi.mocked(updateNurseryDict).mockResolvedValueOnce(updatedItem)
    const onSuccess = vi.fn()
    const onCancel = vi.fn()
    render(
      <NurseryDictForm
        mode="edit"
        initialData={mockItem}
        onSuccess={onSuccess}
        onCancel={onCancel}
      />
    )

    fireEvent.change(screen.getByPlaceholderText("补充说明..."), {
      target: { value: "已更新" },
    })
    fireEvent.click(screen.getByRole("button", { name: /保存/ }))

    await waitFor(() => {
      expect(updateNurseryDict).toHaveBeenCalledOnce()
    })
    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith(expect.objectContaining({ remark: "已更新" }))
    })
  })

  it("TestNurseryDictForm_SpecTemplate_RenderedAsKeyValueInputs", () => {
    const itemWithSpec: NurseryDictItem = {
      ...mockItem,
      spec_template: { 胸径_cm: null },
    }
    render(
      <NurseryDictForm
        mode="edit"
        initialData={itemWithSpec}
        onSuccess={vi.fn()}
        onCancel={vi.fn()}
      />
    )

    // Should render an input for the spec key
    const specInput = screen.getByDisplayValue("胸径_cm")
    expect(specInput).toBeInTheDocument()
  })
})
