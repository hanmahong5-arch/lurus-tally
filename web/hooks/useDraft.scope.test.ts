/**
 * useDraft: flush-on-unmount and tenant/user key isolation.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { useEffect } from "react"
import { renderHook, act, waitFor } from "@testing-library/react"
import { useDraft } from "./useDraft"
import * as idbStorage from "@/lib/draft/idb-storage"

vi.mock("@/lib/draft/idb-storage")

let mockSession: { status: string; data: unknown }
vi.mock("next-auth/react", () => ({ useSession: () => mockSession }))

/** Let the initial IDB load (and any legacy lookup) settle. */
async function loaded() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10))
  })
}

function sessionOf(tenantId: string, id: string) {
  return { status: "authenticated", data: { user: { tenantId, id } } }
}

/** In-memory IDB stand-in. */
function useFakeStore() {
  const store = new Map<string, unknown>()
  vi.mocked(idbStorage.draftGet).mockImplementation(async (k: string) => store.get(k) as never)
  vi.mocked(idbStorage.draftSet).mockImplementation(async (k: string, v: unknown) => {
    store.set(k, v)
  })
  vi.mocked(idbStorage.draftDel).mockImplementation(async (k: string) => {
    store.delete(k)
  })
  return store
}

describe("useDraft scope and flush", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSession = sessionOf("t1", "u1")
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it("unmount flushes the last change without waiting for debounce", async () => {
    const store = useFakeStore()
    const { result, unmount } = renderHook(() => useDraft("draft:x", { n: "a" }))
    await loaded()
    act(() => result.current.setValue({ n: "last" }))
    expect(store.size).toBe(0)
    unmount()
    expect((store.get("draft:x@t1:u1") as { value: unknown }).value).toEqual({ n: "last" })
  })

  it("pagehide and hidden visibility flush immediately", async () => {
    const store = useFakeStore()
    const { result } = renderHook(() => useDraft("draft:x", { n: "a" }))
    await loaded()
    act(() => result.current.setValue({ n: "p1" }))
    window.dispatchEvent(new Event("pagehide"))
    expect((store.get("draft:x@t1:u1") as { value: unknown }).value).toEqual({ n: "p1" })

    act(() => result.current.setValue({ n: "p2" }))
    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true })
    document.dispatchEvent(new Event("visibilitychange"))
    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true })
    expect((store.get("draft:x@t1:u1") as { value: unknown }).value).toEqual({ n: "p2" })
  })

  it("another tenant or user cannot see the draft", async () => {
    const store = useFakeStore()
    const first = renderHook(() => useDraft("draft:x", { n: "a" }))
    await loaded()
    act(() => first.result.current.setValue({ n: "secret" }))
    first.unmount()
    expect(store.size).toBe(1)

    for (const s of [sessionOf("t2", "u1"), sessionOf("t1", "u2")]) {
      mockSession = s
      const other = renderHook(() => useDraft("draft:x", { n: "a" }))
      await waitFor(() => expect(idbStorage.draftGet).toHaveBeenCalledWith(
        `draft:x@${(s.data as { user: { tenantId: string; id: string } }).user.tenantId}:${(s.data as { user: { id: string } }).user.id}`))
      expect(other.result.current.value).toEqual({ n: "a" })
      expect(other.result.current.status).toBe("none")
      other.unmount()
    }
  })

  it("same user gets the draft back after remount", async () => {
    useFakeStore()
    const first = renderHook(() => useDraft("draft:x", { n: "a" }))
    await loaded()
    act(() => first.result.current.setValue({ n: "keep" }))
    first.unmount()
    const again = renderHook(() => useDraft("draft:x", { n: "a" }))
    await waitFor(() => expect(again.result.current.value).toEqual({ n: "keep" }))
  })

  it("legacy unscoped draft is migrated once and removed", async () => {
    const store = useFakeStore()
    store.set("draft:x", { value: { n: "old" }, savedAt: new Date().toISOString() })
    const { result } = renderHook(() => useDraft("draft:x", { n: "a" }))
    await waitFor(() => expect(result.current.value).toEqual({ n: "old" }))
    expect(store.has("draft:x")).toBe(false)
    expect(store.has("draft:x@t1:u1")).toBe(true)

    // A second account never inherits it.
    mockSession = sessionOf("t2", "u9")
    const other = renderHook(() => useDraft("draft:x", { n: "a" }))
    await waitFor(() => expect(idbStorage.draftGet).toHaveBeenCalledWith("draft:x@t2:u9"))
    expect(other.result.current.value).toEqual({ n: "a" })
  })

  it("waits for the session before loading", async () => {
    useFakeStore()
    mockSession = { status: "loading", data: null }
    renderHook(() => useDraft("draft:x", { n: "a" }))
    await new Promise((r) => setTimeout(r, 20))
    expect(idbStorage.draftGet).not.toHaveBeenCalled()
  })

  it("StrictMode double mount with a mount-time setValue keeps the stored draft", async () => {
    const store = useFakeStore()
    store.set("draft:x@t1:u1", { value: { n: "real" }, savedAt: new Date().toISOString() })
    const { result } = renderHook(
      () => {
        const d = useDraft("draft:x", { n: "" })
        const { setValue } = d
        // Page-style persist effect: pushes the blank form on mount.
        useEffect(() => {
          setValue({ n: "" })
          // eslint-disable-next-line react-hooks/exhaustive-deps
        }, [])
        return d
      },
      { reactStrictMode: true },
    )
    await waitFor(() => expect(result.current.value).toEqual({ n: "real" }))
    expect((store.get("draft:x@t1:u1") as { value: unknown }).value).toEqual({ n: "real" })
  })

  it("switching scope while mounted resets to the initial value", async () => {
    const store = useFakeStore()
    store.set("draft:x@t1:u1", { value: { n: "secret-of-t1" }, savedAt: new Date().toISOString() })
    const { result, rerender } = renderHook(() => useDraft("draft:x", { n: "a" }))
    await waitFor(() => expect(result.current.value).toEqual({ n: "secret-of-t1" }))
    mockSession = sessionOf("t2", "u1")
    rerender()
    await waitFor(() => expect(result.current.value).toEqual({ n: "a" }))
    expect(result.current.status).toBe("none")
  })

  it("markSubmitted also removes a leftover legacy key", async () => {
    const store = useFakeStore()
    const { result } = renderHook(() => useDraft("draft:x", { n: "a" }))
    await loaded()
    store.set("draft:x", { value: { n: "old" }, savedAt: new Date().toISOString() })
    await act(async () => {
      await result.current.markSubmitted()
    })
    expect(store.has("draft:x")).toBe(false)
  })
})
