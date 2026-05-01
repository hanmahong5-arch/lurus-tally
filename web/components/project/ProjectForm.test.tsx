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

import { createProject, updateProject } from "@/lib/api/projects"
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
})
