/**
 * Unit tests for ProjectForm component (Story 28.2).
 */
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

// Mock the API
vi.mock("@/lib/api/projects", () => ({
  createProject: vi.fn(),
  updateProject: vi.fn(),
}))

vi.mock("@/lib/api/customers", () => ({
  getCustomer: vi.fn().mockResolvedValue({ id: "c-9", name: "老张绿化" }),
}))

vi.mock("@/components/entity-pickers", () => ({
  CustomerPicker: ({ label, onSelect, onClear }: { label: string; onSelect: (o: { id: string; name: string }) => void; onClear?: () => void }) => (
    <div>
      <button type="button" onClick={() => onClear?.()}>
        clear-customer
      </button>
      <span data-testid="picked">{label}</span>
      <button type="button" onClick={() => onSelect({ id: "c-1", name: "李老板" })}>
        pick-customer
      </button>
    </div>
  ),
}))

import { createProject, updateProject } from "@/lib/api/projects"
import { ApiError } from "@/lib/api/errors"
import ProjectForm from "./ProjectForm"
import type { ProjectItem } from "@/lib/api/projects"

const mockItem: ProjectItem = {
  id: "proj-1",
  tenantId: "tenant-1",
  code: "P001",
  name: "河道绿化",
  status: "active",
  address: "",
  manager: "",
  remark: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
}

describe("ProjectForm", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("TestProjectForm_RequiredFields_ShowsValidationError", async () => {
    const onSuccess = vi.fn()
    const onCancel = vi.fn()
    render(
      <ProjectForm mode="create" onSuccess={onSuccess} onCancel={onCancel} />
    )

    // Submit with empty name and code
    fireEvent.click(screen.getByRole("button", { name: /创建/ }))

    await waitFor(() => {
      expect(screen.getByText("项目名称不能为空")).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText("项目编号不能为空")).toBeInTheDocument()
    })
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it("TestProjectForm_SubmitCreate_CallsCreateApi", async () => {
    vi.mocked(createProject).mockResolvedValueOnce({
      ...mockItem,
      name: "河道绿化",
      code: "P001",
    })
    const onSuccess = vi.fn()
    const onCancel = vi.fn()
    render(
      <ProjectForm mode="create" onSuccess={onSuccess} onCancel={onCancel} />
    )

    fireEvent.change(screen.getByPlaceholderText("请输入项目名称"), {
      target: { value: "河道绿化" },
    })
    fireEvent.change(screen.getByPlaceholderText("请输入项目编号"), {
      target: { value: "P001" },
    })
    fireEvent.click(screen.getByRole("button", { name: /创建/ }))

    await waitFor(() => {
      expect(createProject).toHaveBeenCalledOnce()
    })
    const callArg = vi.mocked(createProject).mock.calls[0][0]
    expect(callArg.name).toBe("河道绿化")
    expect(callArg.code).toBe("P001")
    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith(
        expect.objectContaining({ name: "河道绿化" })
      )
    })
  })

  it("TestProjectForm_SubmitEdit_CallsUpdateApi", async () => {
    const updatedItem = { ...mockItem, remark: "已更新" }
    vi.mocked(updateProject).mockResolvedValueOnce(updatedItem)
    const onSuccess = vi.fn()
    const onCancel = vi.fn()
    render(
      <ProjectForm
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
      expect(updateProject).toHaveBeenCalledOnce()
    })
    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith(
        expect.objectContaining({ remark: "已更新" })
      )
    })
  })

  it("TestProjectForm_CustomerField_HasTodoComment", () => {
    render(
      <ProjectForm
        mode="create"
        onSuccess={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    // The customer field container should be present in the DOM
    const customerField = screen.getByTestId("customer-field")
    expect(customerField).toBeInTheDocument()
  })

  it("TestProjectForm_Customer_PickedNotTypedAsId", async () => {
    vi.mocked(createProject).mockResolvedValueOnce(mockItem)
    const { container } = render(<ProjectForm mode="create" onSuccess={vi.fn()} onCancel={vi.fn()} />)
    expect(container.textContent).not.toMatch(/UUID|客户ID/)

    fireEvent.change(container.querySelector('input[name="code"]')!, { target: { value: "P9" } })
    fireEvent.change(container.querySelector('input[name="name"]')!, { target: { value: "小区绿化" } })
    fireEvent.click(screen.getByText("pick-customer"))
    fireEvent.click(screen.getByRole("button", { name: /创建/ }))

    await waitFor(() => expect(createProject).toHaveBeenCalledOnce())
    expect(vi.mocked(createProject).mock.calls[0][0]).toMatchObject({ customerId: "c-1" })
  })

  it("TestProjectForm_Edit_ShowsCustomerName", async () => {
    render(
      <ProjectForm mode="edit" initialData={{ ...mockItem, customerId: "c-9" }} onSuccess={vi.fn()} onCancel={vi.fn()} />
    )
    await waitFor(() => expect(screen.getByTestId("picked").textContent).toBe("老张绿化"))
  })

  it("TestProjectForm_SubmitError_PlainChinese", async () => {
    vi.mocked(createProject).mockRejectedValueOnce(new ApiError(409, "dup", "x", { error: "duplicate code" }))
    const { container } = render(<ProjectForm mode="create" onSuccess={vi.fn()} onCancel={vi.fn()} />)
    fireEvent.change(container.querySelector('input[name="code"]')!, { target: { value: "P9" } })
    fireEvent.change(container.querySelector('input[name="name"]')!, { target: { value: "x" } })
    fireEvent.click(screen.getByRole("button", { name: /创建/ }))
    expect(await screen.findByText("这个项目编号已经用过了，换一个")).toBeInTheDocument()
  })

  async function submitWith(fill: (c: HTMLElement) => void) {
    const { container } = render(<ProjectForm mode="create" onSuccess={vi.fn()} onCancel={vi.fn()} />)
    fireEvent.change(container.querySelector('input[name="code"]')!, { target: { value: "P9" } })
    fireEvent.change(container.querySelector('input[name="name"]')!, { target: { value: "x" } })
    fill(container)
    fireEvent.click(screen.getByRole("button", { name: /创建/ }))
  }

  it("TestProjectForm_AmountWithThousandsComma_SentClean", async () => {
    vi.mocked(createProject).mockResolvedValueOnce(mockItem)
    await submitWith((c) =>
      fireEvent.change(c.querySelector('input[name="contractAmount"]')!, { target: { value: "1,200" } })
    )
    await waitFor(() => expect(createProject).toHaveBeenCalledOnce())
    expect(vi.mocked(createProject).mock.calls[0][0]).toMatchObject({ contractAmount: "1200" })
  })

  it("TestProjectForm_AmountNotNumber_BlockedBeforeSubmit", async () => {
    await submitWith((c) =>
      fireEvent.change(c.querySelector('input[name="contractAmount"]')!, { target: { value: "1.2万" } })
    )
    expect(await screen.findByText(/合同金额请填写数字/)).toBeInTheDocument()
    expect(createProject).not.toHaveBeenCalled()
  })

  it("TestProjectForm_EndBeforeStart_BlockedBeforeSubmit", async () => {
    await submitWith((c) => {
      fireEvent.change(c.querySelector('input[name="startDate"]')!, { target: { value: "2026-05-02" } })
      fireEvent.change(c.querySelector('input[name="endDate"]')!, { target: { value: "2026-05-01" } })
    })
    expect(await screen.findByText("完工日期不能早于开工日期")).toBeInTheDocument()
    expect(createProject).not.toHaveBeenCalled()
  })

  it("TestProjectForm_EnglishRejection_ReplacedWithFallback", async () => {
    vi.mocked(createProject).mockRejectedValueOnce(
      new ApiError(400, "bad", "x", { error: "project create validate: end_date must be on or after start_date" })
    )
    await submitWith(() => {})
    expect(await screen.findByText("保存没成功，请检查填写的内容")).toBeInTheDocument()
  })

  it("TestProjectForm_EditClearCustomer_KeepsItAndSaysSo", async () => {
    render(
      <ProjectForm mode="edit" initialData={{ ...mockItem, customerId: "c-9" }} onSuccess={vi.fn()} onCancel={vi.fn()} />
    )
    fireEvent.click(screen.getByText("clear-customer"))
    expect(screen.getByText(/暂不支持去掉客户/)).toBeInTheDocument()
  })
})
