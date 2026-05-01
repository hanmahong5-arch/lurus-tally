/**
 * Unit tests for the projects API wrapper (Story 28.2).
 * Backend is mocked via global.fetch — no Go process required.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  listProjects,
  createProject,
  deleteProject,
  restoreProject,
  type ProjectItem,
} from "./projects"

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

function mockFetch(ok: boolean, body: unknown, status?: number) {
  global.fetch = vi.fn().mockResolvedValueOnce({
    ok,
    status: ok ? (status ?? 200) : (status ?? 400),
    json: async () => body,
  })
}

describe("listProjects", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestProjectsApi_List_ParsesPaginatedResponse", async () => {
    mockFetch(true, { items: [mockItem], total: 1 })
    const result = await listProjects({})
    expect(result.items[0].name).toBe("河道绿化")
    expect(result.total).toBe(1)
  })

  it("throws on non-ok response", async () => {
    mockFetch(false, { error: "unauthorized" }, 401)
    await expect(listProjects({})).rejects.toThrow("unauthorized")
  })
})

describe("createProject", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestProjectsApi_Create_Returns201Data", async () => {
    mockFetch(true, { ...mockItem, name: "测试" }, 201)
    const result = await createProject({
      code: "T001",
      name: "测试",
      status: "active",
      address: "",
      manager: "",
      remark: "",
    })
    expect(result.name).toBe("测试")
  })
})

describe("deleteProject", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestProjectsApi_Delete_CallsDeleteMethod", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) })
    await deleteProject("id1")
    const fetchMock = vi.mocked(global.fetch)
    const callUrl = String(fetchMock.mock.calls[0][0])
    const callOptions = fetchMock.mock.calls[0][1] as RequestInit
    expect(callUrl).toContain("id1")
    expect(callOptions.method).toBe("DELETE")
  })
})

describe("restoreProject", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestProjectsApi_Restore_CallsRestoreEndpoint", async () => {
    mockFetch(true, mockItem)
    await restoreProject("id1")
    const fetchMock = vi.mocked(global.fetch)
    const callUrl = String(fetchMock.mock.calls[0][0])
    const callOptions = fetchMock.mock.calls[0][1] as RequestInit
    expect(callUrl).toContain("id1")
    expect(callUrl).toContain("restore")
    expect(callOptions.method).toBe("POST")
  })
})
