import { describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"

// The global setup stubs useConfirm; this file tests the real one.
vi.mock("@/hooks/useConfirm", async () => vi.importActual("@/hooks/useConfirm"))
import { ConfirmProvider, useConfirm } from "@/hooks/useConfirm"

function Probe({ onResult }: { onResult: (v: boolean | null) => void }) {
  const confirm = useConfirm()
  return (
    <button onClick={async () => onResult(await confirm({ title: "测试", confirmText: "好", cancelText: "去修改", distinguishDismiss: true }))}>
      开
    </button>
  )
}

async function run(act: () => void) {
  const results: Array<boolean | null> = []
  render(
    <ConfirmProvider>
      <Probe onResult={(v) => results.push(v)} />
    </ConfirmProvider>,
  )
  fireEvent.click(screen.getByText("开"))
  await screen.findByText("测试")
  act()
  await waitFor(() => expect(results).toHaveLength(1))
  return results[0]
}

describe("useConfirm", () => {
  it("resolves true on the confirm button", async () => {
    expect(await run(() => fireEvent.click(screen.getByText("好")))).toBe(true)
  })

  it("resolves false on the cancel button", async () => {
    expect(await run(() => fireEvent.click(screen.getByText("去修改")))).toBe(false)
  })

  it("resolves null when dismissed with Esc", async () => {
    expect(await run(() => fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" }))).toBeNull()
  })
})
