"use client"

import { useCallback } from "react"
import dynamic from "next/dynamic"

// Lazy-load palette + drawer: both are client-only modals that don't need to
// block first paint. Pulling them out of the dashboard-layout chunk shaves
// ~10-20 kB off every (dashboard)/* route's First Load JS. ⌘K and ⌘J still
// bind on client hydration via useGlobalShortcut inside each component.
const CommandPalette = dynamic(
  () => import("@/components/command-palette/Palette").then((m) => m.CommandPalette),
  { ssr: false, loading: () => null },
)
const AIDrawer = dynamic(
  () => import("./Drawer").then((m) => m.AIDrawer),
  { ssr: false, loading: () => null },
)
const ShortcutHelp = dynamic(
  () => import("@/components/command-palette/ShortcutHelp").then((m) => m.ShortcutHelp),
  { ssr: false, loading: () => null },
)

/**
 * GlobalAI mounts both the CommandPalette and the AIDrawer and wires them.
 *
 * When the user selects "Ask AI: <query>" in the palette, the palette dispatches
 * a custom DOM event "tally:ai-query" that the AIDrawer listens for, which
 * causes the drawer to open and auto-submit the query.
 *
 * Using a DOM event avoids prop-drilling and keeps each component self-contained.
 * Mount this once in the dashboard layout.
 */
export function GlobalAI() {
  const handleAIQuery = useCallback((query: string) => {
    window.dispatchEvent(
      new CustomEvent("tally:ai-query", { detail: { query } })
    )
  }, [])

  return (
    <>
      <CommandPalette onAIQuery={handleAIQuery} />
      <AIDrawer />
      <ShortcutHelp />
    </>
  )
}
