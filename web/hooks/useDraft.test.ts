/**
 * Unit tests for useDraft hook.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { renderHook, act, waitFor } from "@testing-library/react"
import { useDraft } from "./useDraft"
import * as idbStorage from "@/lib/draft/idb-storage"

vi.mock("@/lib/draft/idb-storage")

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000

describe("useDraft", () => {
  beforeEach(() => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    idbStorage._resetIdbAvailableForTest?.()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("TestUseDraft_InitialValue_IsUsedWhenNoDraftExists", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)

    const { result } = renderHook(() =>
      useDraft("draft:test:key", { name: "initial" })
    )

    await waitFor(() => {
      // After mount effect, still returns initial when no draft found.
      expect(result.current.value).toEqual({ name: "initial" })
      expect(result.current.status).toBe("none")
      expect(result.current.restoredAt).toBeNull()
    })
  })

  it("TestUseDraft_RestoredValue_IsReturnedAfterNavigateAway", async () => {
    const savedAt = new Date().toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { name: "restored" },
      savedAt,
    })

    const { result } = renderHook(() =>
      useDraft("draft:test:key", { name: "initial" })
    )

    await waitFor(() => {
      expect(result.current.value).toEqual({ name: "restored" })
      expect(result.current.status).toBe("local")
      expect(result.current.restoredAt).toBeInstanceOf(Date)
    })
  })

  it("TestUseDraft_IdbUnavailable_FallsBackToInitial", async () => {
    vi.mocked(idbStorage.draftGet).mockRejectedValue(
      new Error("IDB unavailable")
    )

    // Should not throw; hook returns initial value.
    const { result } = renderHook(() =>
      useDraft("draft:test:key", { name: "initial" })
    )

    await waitFor(() => {
      expect(result.current.value).toEqual({ name: "initial" })
      expect(result.current.status).toBe("none")
    })
  })

  it("TestUseDraft_MarkSubmitted_ClearsDraft", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)

    const { result } = renderHook(() =>
      useDraft("draft:test:key", { name: "initial" })
    )

    // Set a value first.
    act(() => {
      result.current.setValue({ name: "typed" })
    })

    await act(async () => {
      await result.current.markSubmitted()
    })

    expect(result.current.status).toBe("synced")
    expect(idbStorage.draftDel).toHaveBeenCalledWith("draft:test:key")
  })

  it("setValue updates value and sets status to local", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)

    const { result } = renderHook(() =>
      useDraft("draft:test:key", { name: "initial" })
    )

    await waitFor(() => expect(result.current.status).toBe("none"))

    act(() => {
      result.current.setValue({ name: "changed" })
    })

    expect(result.current.value).toEqual({ name: "changed" })
    expect(result.current.status).toBe("local")
  })

  it("discardDraft resets value and status to none", async () => {
    const savedAt = new Date().toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { name: "restored" },
      savedAt,
    })

    const { result } = renderHook(() =>
      useDraft("draft:test:key", { name: "initial" })
    )

    await waitFor(() => expect(result.current.status).toBe("local"))

    await act(async () => {
      await result.current.discardDraft()
    })

    expect(result.current.value).toEqual({ name: "initial" })
    expect(result.current.status).toBe("none")
    expect(result.current.restoredAt).toBeNull()
    expect(idbStorage.draftDel).toHaveBeenCalledWith("draft:test:key")
  })

  it("drafts older than 7 days are discarded silently", async () => {
    const oldDate = new Date(Date.now() - SEVEN_DAYS_MS - 1000).toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { name: "old draft" },
      savedAt: oldDate,
    })

    const { result } = renderHook(() =>
      useDraft("draft:test:key", { name: "initial" })
    )

    await waitFor(() => {
      // Old draft is discarded; initial value is used.
      expect(result.current.status).toBe("none")
      expect(result.current.value).toEqual({ name: "initial" })
    })
    expect(idbStorage.draftDel).toHaveBeenCalledWith("draft:test:key")
  })
})
