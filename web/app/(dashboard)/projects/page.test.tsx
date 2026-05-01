/**
 * Unit tests for ProjectsPage (Story 28.2).
 */
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

// Mock API module
vi.mock("@/lib/api/projects", () => ({
  listProjects: vi.fn(),
  deleteProject: vi.fn(),
  restoreProject: vi.fn(),
}))

// Mock ProjectForm to keep tests focused on the page
vi.mock("@/components/project/ProjectForm", () => ({
  default: () => <div>MockProjectForm</div>,
}))

import { listProjects } from "@/lib/api/projects"
import ProjectsPage from "./page"
import type { ProjectItem } from "@/lib/api/projects"

function makeItem(i: number): ProjectItem {
  return {
    id: `proj-${i}`,
    tenantId: "tenant-1",
    code: `P00${i}`,
    name: `项目${i}`,
    status: "active",
    address: "",
    manager: "",
    remark: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  }
}

describe("ProjectsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("TestProjectsPage_Renders_SearchAndGrid", async () => {
    vi.mocked(listProjects).mockResolvedValueOnce({ items: [], total: 0 })
    render(<ProjectsPage />)
    // Search input present immediately
    expect(
      screen.getByPlaceholderText("搜索项目名称或编号...")
    ).toBeInTheDocument()
    // Status filter select present
    await waitFor(() => {
      expect(screen.getByDisplayValue("全部")).toBeInTheDocument()
    })
    // New project button present
    expect(screen.getByRole("button", { name: /新建项目/ })).toBeInTheDocument()
  })

  it("TestProjectsPage_CardsRendered", async () => {
    const items = [makeItem(1), makeItem(2), makeItem(3)]
    vi.mocked(listProjects).mockResolvedValueOnce({ items, total: 3 })
    render(<ProjectsPage />)
    await waitFor(() => {
      const cards = screen.getAllByTestId("project-card")
      expect(cards).toHaveLength(3)
    })
  })

  it("TestProjectsPage_EmptyState_Shown", async () => {
    vi.mocked(listProjects).mockResolvedValueOnce({ items: [], total: 0 })
    render(<ProjectsPage />)
    await waitFor(() => {
      expect(screen.getByText(/暂无项目/)).toBeInTheDocument()
    })
  })

  it("TestProjectsPage_Search_CallsApiWithQuery", async () => {
    vi.mocked(listProjects).mockResolvedValue({ items: [], total: 0 })
    render(<ProjectsPage />)
    await waitFor(() => {
      expect(listProjects).toHaveBeenCalledOnce()
    })

    fireEvent.change(screen.getByPlaceholderText("搜索项目名称或编号..."), {
      target: { value: "河道" },
    })

    // Wait up to 1s for debounce to fire and listProjects to be called with query
    await waitFor(
      () => {
        const calls = vi.mocked(listProjects).mock.calls
        const hasQuery = calls.some((call) => call[0]?.q === "河道")
        expect(hasQuery).toBe(true)
      },
      { timeout: 1000 }
    )
  })
})
