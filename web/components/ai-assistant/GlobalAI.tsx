"use client"

import { useCallback, useEffect, useState } from "react"
import dynamic from "next/dynamic"
import { getTodoSummary } from "@/lib/api/todo"
import { useRole } from "@/hooks/use-role"

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
 * The assistant (drawer and the palette's "ask" row) is only mounted once the
 * todo summary says `ai_enabled`; until then, and on any failed read, it stays
 * hidden so a switched-off shop never sees an entry that answers 503.
 *
 * Using a DOM event avoids prop-drilling and keeps each component self-contained.
 * Mount this once in the dashboard layout.
 */
export function GlobalAI() {
  const { can } = useRole()
  const [aiEnabled, setAiEnabled] = useState(false)
  useEffect(() => {
    const ctrl = new AbortController()
    getTodoSummary({ signal: ctrl.signal })
      .then((s) => {
        if (!ctrl.signal.aborted) setAiEnabled(s?.ai_enabled === true)
      })
      .catch(() => {})
    return () => ctrl.abort()
  }, [])

  const handleAIQuery = useCallback((query: string) => {
    window.dispatchEvent(
      new CustomEvent("tally:ai-query", { detail: { query } })
    )
  }, [])

  const aiOn = aiEnabled && can("ai.use")

  return (
    <>
      <CommandPalette onAIQuery={handleAIQuery} aiEnabled={aiOn} />
      {aiOn && <AIDrawer />}
      <ShortcutHelp aiEnabled={aiOn} />
    </>
  )
}
