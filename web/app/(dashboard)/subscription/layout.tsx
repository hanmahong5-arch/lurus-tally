import Link from "next/link"
import { auth } from "@/auth"
import { PageContainer } from "@/components/ui/page-container"
import { EmptyState } from "@/components/ui/empty-state"
import { buttonVariants } from "@/components/ui/button"
import { normalizeRole, roleCan } from "@/lib/roles"

/**
 * Plans and payment are the owner's. Until the session has told us the role
 * nothing is blocked (the backend still decides); a known non-owner gets a
 * short note instead of a page full of buttons that would only answer 403.
 */
export default async function SubscriptionLayout({ children }: { children: React.ReactNode }) {
  const session = await auth()
  const role = normalizeRole(session?.user?.role, session?.user?.isOwner === true)
  if (role !== null && !roleCan(role, "billing.manage")) {
    return (
      <PageContainer>
        <EmptyState
          title="套餐和付费只有老板能看"
          description="需要开通或续费，请告诉老板"
          action={
            <Link href="/dashboard" className={buttonVariants({ variant: "outline" })}>
              回到首页
            </Link>
          }
        />
      </PageContainer>
    )
  }
  return <>{children}</>
}
