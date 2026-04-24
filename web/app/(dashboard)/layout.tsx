import { DashboardSidebar } from "./sidebar"

/**
 * Dashboard layout — wraps all routes in the (dashboard) group with a sidebar + header.
 * The sidebar is profile-aware (retail users see POS link).
 *
 * This is a Server Component shell; DashboardSidebar is the Client Component that
 * reads useProfile() to show/hide the POS link.
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
    </div>
  )
}
