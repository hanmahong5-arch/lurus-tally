/**
 * Unit tests for the nursery dictionary API wrapper (Story 28.1).
 * Backend is mocked via global.fetch — no Go process required.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  listNurseryDict,
  createNurseryDict,
  deleteNurseryDict,
  restoreNurseryDict,
  type NurseryDictItem,
} from "./nursery-dict"

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

function mockFetch(ok: boolean, body: unknown, status?: number) {
  global.fetch = vi.fn().mockResolvedValueOnce({
    ok,
    status: ok ? (status ?? 200) : (status ?? 400),
    json: async () => body,
  })
}

describe("listNurseryDict", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    // jsdom does not provide window.location.origin reliably; patch it
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestNurseryDictApi_List_ParsesPaginatedResponse", async () => {
    mockFetch(true, { items: [mockItem], total: 1 })
    const result = await listNurseryDict({})
    expect(result.items[0].name).toBe("红枫")
    expect(result.total).toBe(1)
  })

  it("throws on non-ok response", async () => {
    mockFetch(false, { error: "unauthorized" }, 401)
    await expect(listNurseryDict({})).rejects.toThrow("unauthorized")
  })
})

describe("createNurseryDict", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestNurseryDictApi_Create_Returns201Data", async () => {
    mockFetch(true, { ...mockItem, name: "测试" }, 201)
    const result = await createNurseryDict({
      name: "测试",
      latin_name: "",
      family: "",
      genus: "",
      type: "tree",
      is_evergreen: false,
      climate_zones: [],
      best_season: [0, 0],
      spec_template: {},
      photo_url: "",
      remark: "",
    })
    expect(result.name).toBe("测试")
  })
})

describe("deleteNurseryDict", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestNurseryDictApi_Delete_CallsDeleteEndpoint", async () => {
    global.fetch = vi.fn().mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) })
    await deleteNurseryDict("id1")
    const fetchMock = vi.mocked(global.fetch)
    const callUrl = String(fetchMock.mock.calls[0][0])
    const callOptions = fetchMock.mock.calls[0][1] as RequestInit
    expect(callUrl).toContain("id1")
    expect(callOptions.method).toBe("DELETE")
  })
})

describe("restoreNurseryDict", () => {
  beforeEach(() => {
    global.fetch = vi.fn()
    Object.defineProperty(window, "location", {
      value: { origin: "http://localhost:3000" },
      writable: true,
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it("TestNurseryDictApi_Restore_CallsRestoreEndpoint", async () => {
    mockFetch(true, mockItem)
    await restoreNurseryDict("id1")
    const fetchMock = vi.mocked(global.fetch)
    const callUrl = String(fetchMock.mock.calls[0][0])
    const callOptions = fetchMock.mock.calls[0][1] as RequestInit
    expect(callUrl).toContain("id1")
    expect(callUrl).toContain("restore")
    expect(callOptions.method).toBe("POST")
  })
})
