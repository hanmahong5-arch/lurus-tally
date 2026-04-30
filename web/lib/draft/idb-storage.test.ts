/**
 * Unit tests for idb-storage.ts
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import {
  draftGet,
  draftSet,
  draftDel,
  isDraftStorageAvailable,
  _resetIdbAvailableForTest,
} from "./idb-storage"

// Mock idb-keyval so tests run in jsdom without a real IDB implementation.
vi.mock("idb-keyval", () => {
  const store: Map<string, unknown> = new Map()
  return {
    get: vi.fn(async (key: string) => store.get(key)),
    set: vi.fn(async (key: string, value: unknown) => store.set(key, value)),
    del: vi.fn(async (key: string) => store.delete(key)),
    _store: store,
  }
})

import * as idbKeyval from "idb-keyval"

// Cast for convenience.
const mockStore = (idbKeyval as unknown as { _store: Map<string, unknown> })._store

function securityError(): DOMException {
  return new DOMException("security error", "SecurityError")
}

function quotaError(): DOMException {
  return new DOMException("quota exceeded", "QuotaExceededError")
}

describe("idb-storage", () => {
  beforeEach(() => {
    mockStore.clear()
    vi.mocked(idbKeyval.get).mockImplementation(async (key: string) =>
      mockStore.get(key)
    )
    vi.mocked(idbKeyval.set).mockImplementation(
      async (key: string, value: unknown) => {
        mockStore.set(key, value)
      }
    )
    vi.mocked(idbKeyval.del).mockImplementation(async (key: string) => {
      mockStore.delete(key)
    })
    _resetIdbAvailableForTest()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    _resetIdbAvailableForTest()
  })

  it("draftGet returns undefined on fresh store", async () => {
    const result = await draftGet("draft:test:key")
    expect(result).toBeUndefined()
  })

  it("draftSet then draftGet returns the stored value", async () => {
    await draftSet("draft:test:key", { name: "hello" })
    const result = await draftGet<{ name: string }>("draft:test:key")
    expect(result).toEqual({ name: "hello" })
  })

  it("draftDel clears the stored value", async () => {
    await draftSet("draft:test:key", { name: "hello" })
    await draftDel("draft:test:key")
    const result = await draftGet("draft:test:key")
    expect(result).toBeUndefined()
  })

  it("isDraftStorageAvailable returns true when IDB is working", () => {
    expect(isDraftStorageAvailable()).toBe(true)
  })

  it("draftGet resolves without throwing when IDB throws SecurityError", async () => {
    vi.mocked(idbKeyval.get).mockRejectedValueOnce(securityError())
    const result = await draftGet("draft:test:key")
    expect(result).toBeUndefined()
    expect(isDraftStorageAvailable()).toBe(false)
  })

  it("draftSet resolves without throwing when IDB throws SecurityError", async () => {
    vi.mocked(idbKeyval.set).mockRejectedValueOnce(securityError())
    await expect(draftSet("draft:test:key", "value")).resolves.toBeUndefined()
    expect(isDraftStorageAvailable()).toBe(false)
  })

  it("draftDel resolves without throwing when IDB throws QuotaExceededError", async () => {
    vi.mocked(idbKeyval.del).mockRejectedValueOnce(quotaError())
    await expect(draftDel("draft:test:key")).resolves.toBeUndefined()
    expect(isDraftStorageAvailable()).toBe(false)
  })

  it("subsequent calls are no-ops after idbAvailable is set false", async () => {
    // Trigger a SecurityError on get to disable IDB.
    vi.mocked(idbKeyval.get).mockRejectedValueOnce(securityError())
    await draftGet("draft:test:key")
    expect(isDraftStorageAvailable()).toBe(false)

    // Clear call counts — the three calls from beforeEach setup don't matter;
    // what matters is that NO idb-keyval functions are invoked after the flag flips.
    vi.mocked(idbKeyval.set).mockClear()
    vi.mocked(idbKeyval.del).mockClear()
    vi.mocked(idbKeyval.get).mockClear()

    await draftSet("draft:test:key2", "value")
    await draftDel("draft:test:key2")
    const result = await draftGet("draft:test:key2")
    expect(result).toBeUndefined()
    expect(idbKeyval.set).not.toHaveBeenCalled()
    expect(idbKeyval.del).not.toHaveBeenCalled()
    expect(idbKeyval.get).not.toHaveBeenCalled()
  })
})
