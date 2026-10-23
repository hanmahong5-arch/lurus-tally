import Link from "next/link"
import { BackOfficeLink, SignOutButton } from "@/components/pos/pos-header-actions"
import { InstallHint } from "@/components/install-hint"
import { SubscriptionBanner } from "@/components/subscription-banner"
import { OfflineBanner } from "@/components/ui/offline-banner"
import { WeChatOpenHint } from "@/components/wechat-open-hint"
import { CashierModeControl, CashierModeProvider, CashierModeReadNotice, HideInCashierMode } from "@/components/cashier-mode-gate"

/**
 * POS layout — independent route segment, does NOT inherit the dashboard layout.
 *
 * Server Component guard: reads session and redirects cross_border/hybrid profiles.
 * Retail-only guard is enforced here; middleware.ts provides a second layer.
 *
 * Story 2.1 TODO: uncomment auth() guard once profileType is reliably set in the JWT.
 * Currently session.user.profileType may be null for existing users until re-login,
 * so we skip the server-side redirect to avoid locking out retail users.
 */
export default async function POSLayout({
  children,
}: {
  children: React.ReactNode
}) {
  // Story 2.1 TODO: enable when profileType is reliably populated in JWT:
  // const session = await auth()
  // if (!session?.user) redirect('/login')
  // if (session.user.profileType && session.user.profileType !== 'retail') {
  //   redirect('/dashboard?error=pos-retail-only')
  // }

  return (
    // One read of the mode shared by the header buttons and the pages below.
    <CashierModeProvider>
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-900" data-testid="pos-layout">
      {/* Single offline-detection implementation, shared with the dashboard layout. */}
      <OfflineBanner />
      <WeChatOpenHint />
      {/* Cashiers live on /pos; surface install-to-home-screen here (also in account drawer). */}
      <div className="px-3 pt-2 sm:px-4">
        <InstallHint />
      </div>
      {/* Same lapsed-plan strip as the back office; the checkout buttons read the same status. */}
      <SubscriptionBanner />
      <header className="absolute right-2 top-1.5 z-10 flex items-center gap-1.5">
        <Link
          href="/pos/history"
          className="inline-flex min-h-[36px] items-center rounded-md px-3 py-1.5 text-xs text-zinc-500 hover:bg-zinc-200 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100 transition-colors"
        >
          今日结单
        </Link>
        {/* In cashier mode the way back to the back office is the PIN-protected exit. */}
        <HideInCashierMode>
          <BackOfficeLink />
        </HideInCashierMode>
        <SignOutButton />
        <CashierModeReadNotice />
        <CashierModeControl variant="exit" />
      </header>
      {children}
    </div>
    </CashierModeProvider>
  )
}
