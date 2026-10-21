import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

const getTodoSummary = vi.fn()
vi.mock("@/lib/api/todo", () => ({ getTodoSummary: (...a: unknown[]) => getTodoSummary(...a) }))

// Resolve the lazy imports synchronously-enough for jsdom.
vi.mock("next/dynamic", async () => {
  const React = await import("react")
  return {
    default: (loader: () => Promise<React.ComponentType<Record<string, unknown>>>) => {
      function Lazy(props: Record<string, unknown>) {
        const [C, setC] = React.useState<React.ComponentType<Record<string, unknown>> | null>(null)
        React.useEffect(() => {
          loader().then((c) => setC(() => c))
        }, [])
        return C ? <C {...props} /> : null
      }
      return Lazy
    },
  }
})
const roleState = vi.hoisted(() => ({ role: "owner" as string }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: roleState.role, isOwner: roleState.role === "owner" } } }),
}))
vi.mock("@/components/command-palette/Palette", () => ({
  CommandPalette: (p: { aiEnabled?: boolean }) => <div data-testid="palette" data-ai={String(p.aiEnabled)} />,
}))
vi.mock("@/components/command-palette/ShortcutHelp", () => ({ ShortcutHelp: () => null }))
vi.mock("./Drawer", () => ({ AIDrawer: () => <div data-testid="ai-drawer" /> }))

import { GlobalAI } from "./GlobalAI"

describe("GlobalAI", () => {
  beforeEach(() => {
    getTodoSummary.mockReset()
    roleState.role = "owner"
  })

  it("keeps the assistant away from a cashier even when the shop has AI on", async () => {
    roleState.role = "cashier"
    getTodoSummary.mockResolvedValue({ ai_enabled: true })
    render(<GlobalAI />)
    await waitFor(() => expect(getTodoSummary).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.getByTestId("palette")).toHaveAttribute("data-ai", "false")
    expect(screen.queryByTestId("ai-drawer")).toBeNull()
  })

  it("mounts no drawer and turns the palette's ask row off when ai_enabled is false", async () => {
    getTodoSummary.mockResolvedValue({ ai_enabled: false })
    render(<GlobalAI />)
    await waitFor(() => expect(screen.getByTestId("palette")).toHaveAttribute("data-ai", "false"))
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByTestId("ai-drawer")).toBeNull()
  })

  it("stays hidden when the summary read fails", async () => {
    getTodoSummary.mockRejectedValue(new Error("x"))
    render(<GlobalAI />)
    await waitFor(() => expect(screen.getByTestId("palette")).toHaveAttribute("data-ai", "false"))
    expect(screen.queryByTestId("ai-drawer")).toBeNull()
  })

  it("mounts the drawer and enables the ask row when ai_enabled is true", async () => {
    getTodoSummary.mockResolvedValue({ ai_enabled: true })
    render(<GlobalAI />)
    expect(await screen.findByTestId("ai-drawer")).toBeInTheDocument()
    expect(screen.getByTestId("palette")).toHaveAttribute("data-ai", "true")
  })
})
