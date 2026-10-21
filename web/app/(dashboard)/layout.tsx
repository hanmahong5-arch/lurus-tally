import { auth } from "@/auth"
import { DashboardSidebar, MobileNav } from "./sidebar"
import { GlobalAI } from "@/components/ai-assistant/GlobalAI"
import { UndoToastProvider } from "@/components/undo/UndoToastProvider"
import { ProfileProvider, type ProfileType } from "@/lib/profile"
import { OfflineBanner } from "@/components/ui/offline-banner"
import { AccountDrawerProvider } from "@/components/account/account-drawer-provider"
import { AccountDrawer } from "@/components/account/account-drawer"
import { CashierModeContentGate, CashierModeProvider } from "@/components/cashier-mode-gate"
import { SignOutWatcher } from "@/components/account/signout-watcher"
import { RouteTransition } from "@/components/ui/route-transition"
import { SubscriptionBanner } from "@/components/subscription-banner"
import { WeChatOpenHint } from "@/components/wechat-open-hint"

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
        <AccountDrawerProvider>
          <CashierModeProvider>
          {/* OfflineBanner: fixed top strip, shown only when navigator.onLine is false */}
          <OfflineBanner />
          {/* Shared till: another tab signing out must not leave this back-office screen up for the next person */}
          <SignOutWatcher />
          <div className="flex h-screen overflow-hidden">
            <DashboardSidebar />
            <main className="flex-1 overflow-y-auto">
              <WeChatOpenHint />
              <SubscriptionBanner />
              <MobileNav />
              <CashierModeContentGate>
                <RouteTransition>{children}</RouteTransition>
              </CashierModeContentGate>
            </main>
            {/* AI assistant: ⌘K Command Palette + Cmd+J Drawer */}
            <GlobalAI />
            {/* Tier 2 account drawer — opened by sidebar AccountCard / palette */}
            <AccountDrawer />
          </div>
          </CashierModeProvider>
        </AccountDrawerProvider>
      </ProfileProvider>
    </UndoToastProvider>
  )
}
