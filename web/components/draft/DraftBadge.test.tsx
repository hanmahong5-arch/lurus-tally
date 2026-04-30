/**
 * Unit tests for DraftBadge and DraftRestoreToast components.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { DraftBadge } from "./DraftBadge"
import { DraftRestoreToast } from "./DraftRestoreToast"
import * as idbStorage from "@/lib/draft/idb-storage"

vi.mock("@/lib/draft/idb-storage")

describe("DraftBadge", () => {
  beforeEach(() => {
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('status=local shows "未保存"', () => {
    render(<DraftBadge status="local" />)
    expect(screen.getByText("未保存")).toBeTruthy()
  })

  it('status=synced shows "已同步"', () => {
    render(<DraftBadge status="synced" />)
    expect(screen.getByText("已同步")).toBeTruthy()
  })

  it("status=none renders nothing", () => {
    const { container } = render(<DraftBadge status="none" />)
    expect(container.firstChild).toBeNull()
  })

  it("renders nothing when IDB is unavailable regardless of status", () => {
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(false)
    const { container } = render(<DraftBadge status="local" />)
    expect(container.firstChild).toBeNull()
  })
})

describe("DraftRestoreToast", () => {
  it("renders nothing when restoredAt is null", () => {
    const { container } = render(
      <DraftRestoreToast restoredAt={null} onDiscard={vi.fn()} />
    )
    expect(container.firstChild).toBeNull()
  })

  it('shows "已恢复" banner when restoredAt is set', () => {
    const restoredAt = new Date(Date.now() - 2 * 60_000) // 2 minutes ago
    render(<DraftRestoreToast restoredAt={restoredAt} onDiscard={vi.fn()} />)
    expect(screen.getByText(/已恢复/)).toBeTruthy()
    expect(screen.getByText(/分钟前的草稿/)).toBeTruthy()
  })

  it('calls onDiscard when "放弃" is clicked', () => {
    const onDiscard = vi.fn()
    const restoredAt = new Date(Date.now() - 60_000)
    render(<DraftRestoreToast restoredAt={restoredAt} onDiscard={onDiscard} />)
    fireEvent.click(screen.getByText("放弃"))
    expect(onDiscard).toHaveBeenCalledOnce()
  })
})
