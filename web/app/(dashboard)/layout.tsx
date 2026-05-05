import { auth } from "@/auth"
import { DashboardSidebar } from "./sidebar"
import { GlobalAI } from "@/components/ai-assistant/GlobalAI"
import { UndoToastProvider } from "@/components/undo/UndoToastProvider"
import { ProfileProvider, type ProfileType } from "@/lib/profile"

/**
 * Dashboard layout — wraps all routes in the (dashboard) group with a sidebar + header.
 * The sidebar is profile-aware (retail users see POS link).
 *
 * GlobalAI mounts the ⌘K Command Palette and AI Drawer globally on every
 * dashboard page. No per-page wiring required.
 *
 * Profile: read from NextAuth session (server-side, populated by jwt callback
 * via /api/v1/me). Passed to ProfileProvider so client components see the
 * real profile without a flicker. When session.user.profileType is null
 * (first-time user), middleware should already have redirected to /setup —
 * this null path is reachable only in dev mode without auth.
 *
 * This is a Server Component shell; DashboardSidebar and GlobalAI are Client Components.
 */
export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const session = await auth()
  const profileType = (session?.user?.profileType ?? null) as ProfileType

  return (
    <UndoToastProvider>
      <ProfileProvider value={{ profileType }}>
        <div className="flex h-screen overflow-hidden">
          <DashboardSidebar />
          <main className="flex-1 overflow-y-auto">{children}</main>
          {/* AI assistant: ⌘K Command Palette + Cmd+J Drawer */}
          <GlobalAI />
        </div>
      </ProfileProvider>
    </UndoToastProvider>
  )
}
