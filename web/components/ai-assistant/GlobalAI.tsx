"use client"

import { useCallback } from "react"
import { AIDrawer } from "./Drawer"
import { CommandPalette } from "@/components/command-palette/Palette"

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
    </>
  )
}
