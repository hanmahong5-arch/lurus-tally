/**
 * Thin wrapper around idb-keyval that degrades silently when IndexedDB is
 * unavailable (private-browsing SecurityError, QuotaExceededError, SSR).
 *
 * The module-level `idbAvailable` flag is set to false on the first IDB error
 * and all subsequent calls become no-ops to avoid repeated error-throwing.
 */
import { get, set, del } from "idb-keyval"

// Tracks whether IDB is functional in the current browser context.
let idbAvailable = true

/** Returns true if IDB storage is operational in this browser context. */
export function isDraftStorageAvailable(): boolean {
  return idbAvailable
}

/**
 * Retrieves a draft value by key.
 * Returns undefined if the key is absent or IDB is unavailable.
 */
export async function draftGet<T>(key: string): Promise<T | undefined> {
  if (!idbAvailable) return undefined
  if (typeof window === "undefined") return undefined
  try {
    return await get<T>(key)
  } catch (err) {
    if (isStorageError(err)) {
      idbAvailable = false
      return undefined
    }
    throw err
  }
}

/**
 * Persists a draft value by key.
 * Silently no-ops if IDB is unavailable.
 */
export async function draftSet<T>(key: string, value: T): Promise<void> {
  if (!idbAvailable) return
  if (typeof window === "undefined") return
  try {
    await set(key, value)
  } catch (err) {
    if (isStorageError(err)) {
      idbAvailable = false
      return
    }
    throw err
  }
}

/**
 * Deletes a draft entry by key.
 * Silently no-ops if IDB is unavailable.
 */
export async function draftDel(key: string): Promise<void> {
  if (!idbAvailable) return
  if (typeof window === "undefined") return
  try {
    await del(key)
  } catch (err) {
    if (isStorageError(err)) {
      idbAvailable = false
      return
    }
    throw err
  }
}

/** Returns true for storage-related errors that warrant graceful degradation. */
function isStorageError(err: unknown): boolean {
  if (err instanceof DOMException) {
    return err.name === "SecurityError" || err.name === "QuotaExceededError"
  }
  return false
}

/**
 * Resets the idbAvailable flag. Exported for test teardown only.
 * Do not call in production code.
 */
export function _resetIdbAvailableForTest(): void {
  idbAvailable = true
}
