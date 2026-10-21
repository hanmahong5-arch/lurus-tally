"use client"

import { useEffect, useState, type ReactNode } from "react"

import { useRole, type Perm } from "@/hooks/use-role"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { EmptyState } from "@/components/ui/empty-state"

/** How long a gated page waits for the role before suggesting a refresh. */
const ROLE_WAIT_MS = 8000

interface RoleGateProps {
  perm: Perm
  title: string
  subtitle?: string
  /** The note shown instead of the page, e.g. "收付款只对老板、店长和财务开放". */
  notice: string
  /** True when the page asks for restricted data on mount: nothing is shown until the role is known. */
  waitForRole?: boolean
  children: ReactNode
}

/**
 * Page-level gate: a member whose role lacks the permission sees a short note
 * instead of a page that would only answer 403. The backend still enforces it.
 */
export function RoleGate({ perm, title, subtitle, notice, waitForRole = false, children }: RoleGateProps) {
  const { known, can } = useRole()
  const [timedOut, setTimedOut] = useState(false)
  useEffect(() => {
    if (known || !waitForRole) return
    const t = setTimeout(() => setTimedOut(true), ROLE_WAIT_MS)
    return () => clearTimeout(t)
  }, [known, waitForRole])
  if (!known) {
    if (!waitForRole) return <>{children}</>
    return (
      <div className="p-6 text-sm text-muted-foreground">
        {timedOut ? "暂时无法确认你的岗位，请刷新页面重试。" : "加载中..."}
      </div>
    )
  }
  if (!can(perm)) {
    return (
      <PageContainer width="default">
        <PageHeader title={title} subtitle={subtitle} />
        <EmptyState title={notice} description="需要时，请联系老板或店长。" />
      </PageContainer>
    )
  }
  return <>{children}</>
}
