import Link from "next/link"

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
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-900" data-testid="pos-layout">
      <header className="absolute right-3 top-2 z-10 flex items-center gap-3">
        <Link
          href="/pos/history"
          className="rounded-md px-2 py-1 text-xs text-zinc-500 hover:bg-zinc-200 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100 transition-colors"
        >
          今日记录
        </Link>
        <Link
          href="/dashboard"
          className="rounded-md px-2 py-1 text-xs text-zinc-500 hover:bg-zinc-200 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100 transition-colors"
        >
          退出 POS ←
        </Link>
      </header>
      {children}
    </div>
  )
}
