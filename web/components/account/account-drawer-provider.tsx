"use client"

import { createContext, useContext, useMemo, useState } from "react"

interface AccountDrawerContextValue {
  open: boolean
  openDrawer: () => void
  closeDrawer: () => void
  toggleDrawer: () => void
}

const NOOP_VALUE: AccountDrawerContextValue = {
  open: false,
  openDrawer: () => {},
  closeDrawer: () => {},
  toggleDrawer: () => {},
}

const AccountDrawerContext = createContext<AccountDrawerContextValue | null>(null)

/**
 * AccountDrawerProvider owns the visibility state of the Tier 2 account
 * drawer. The sidebar card (Tier 1) toggles it open, the drawer itself closes
 * on backdrop click / ESC, and external code (e.g. ⌘K palette) can reach it
 * via useAccountDrawer().
 */
export function AccountDrawerProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  const value = useMemo<AccountDrawerContextValue>(
    () => ({
      open,
      openDrawer: () => setOpen(true),
      closeDrawer: () => setOpen(false),
      toggleDrawer: () => setOpen((o) => !o),
    }),
    [open],
  )
  return (
    <AccountDrawerContext.Provider value={value}>{children}</AccountDrawerContext.Provider>
  )
}

export function useAccountDrawer(): AccountDrawerContextValue {
  return useContext(AccountDrawerContext) ?? NOOP_VALUE
}
