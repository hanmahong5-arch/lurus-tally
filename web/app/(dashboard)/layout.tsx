import { DashboardSidebar } from "./sidebar"
import { GlobalAI } from "@/components/ai-assistant/GlobalAI"

/**
 * Dashboard layout — wraps all routes in the (dashboard) group with a sidebar + header.
 * The sidebar is profile-aware (retail users see POS link).
 *
 * GlobalAI mounts the ⌘K Command Palette and AI Drawer globally on every
 * dashboard page. No per-page wiring required.
 *
 * This is a Server Component shell; DashboardSidebar and GlobalAI are Client Components.
 *
 * Story 2.1 TODO: pass profileType from server session → ProfileProvider here
 * so the initial value is correct without client-side flicker.
 */
export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <div className="flex h-screen overflow-hidden">
      <DashboardSidebar />
      <main className="flex-1 overflow-y-auto">{children}</main>
      {/* AI assistant: ⌘K Command Palette + Cmd+J Drawer */}
      <GlobalAI />
    </div>
  )
}
