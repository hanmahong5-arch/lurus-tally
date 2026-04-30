/**
 * Global in-memory undo stack for destructive UI actions.
 *
 * Rules:
 *  - Max depth: 10 entries (oldest evicted when full)
 *  - Expiry: entries older than 30s are treated as non-existent
 *  - Singleton: exported as globalUndoStack; survives React re-renders
 */

export type UndoAction =
  | { type: "delete_product"; id: string; name: string; revert: () => Promise<void> }
  | { type: "cancel_purchase"; id: string; billNo: string; revert: () => Promise<void> }

export interface UndoEntry {
  action: UndoAction
  /** Timestamp at which the entry was pushed (Date.now()). */
  pushedAt: number
}

const MAX_DEPTH = 10
const EXPIRY_MS = 30_000

class UndoStack {
  private entries: UndoEntry[] = []

  /** Remove entries older than EXPIRY_MS. */
  private pruneExpired(): void {
    const cutoff = Date.now() - EXPIRY_MS
    this.entries = this.entries.filter((e) => e.pushedAt > cutoff)
  }

  /**
   * Push a new action onto the stack.
   * When at MAX_DEPTH, the oldest entry is evicted first.
   */
  push(action: UndoAction): void {
    this.pruneExpired()
    if (this.entries.length >= MAX_DEPTH) {
      this.entries.shift() // evict oldest
    }
    this.entries.push({ action, pushedAt: Date.now() })
  }

  /**
   * Pop the most recent non-expired entry.
   * Returns undefined when the stack is empty or all entries have expired.
   */
  pop(): UndoEntry | undefined {
    this.pruneExpired()
    return this.entries.pop()
  }

  /** Peek at the most recent entry without removing it. */
  peek(): UndoEntry | undefined {
    this.pruneExpired()
    return this.entries[this.entries.length - 1]
  }

  /** Current number of non-expired entries. */
  size(): number {
    this.pruneExpired()
    return this.entries.length
  }

  /** Reset stack state — for use in tests only. */
  resetForTest(): void {
    this.entries = []
  }
}

export const globalUndoStack = new UndoStack()
