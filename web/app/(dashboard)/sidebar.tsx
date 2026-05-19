"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { usePathname } from "next/navigation"
import { useProfile } from "@/lib/profile"

interface NavItem {
  href: string
  label: string
  icon?: string
  /** When set, this item is only shown to tenants whose profileType is in the list. */
  industry?: string[]
}

const BASE_NAV_ITEMS: NavItem[] = [
  { href: "/products", label: "商品管理", icon: "📦" },
  { href: "/stock", label: "库存", icon: "🏬" },
  { href: "/purchases", label: "采购管理", icon: "🛒" },
  { href: "/sales", label: "销售管理", icon: "📊" },
  { href: "/finance/exchange-rates", label: "财务管理", icon: "💰" },
  { href: "/subscription", label: "订阅与计费", icon: "💳" },
  { href: "/suppliers", label: "供应商", icon: "🏭" },
  { href: "/warehouses", label: "仓库", icon: "🏪" },
  { href: "/dictionary", label: "苗木字典", icon: "🌿", industry: ["horticulture"] },
  { href: "/projects", label: "项目", icon: "🏗️" },
  { href: "/settings/api-keys", label: "API 密钥", icon: "🔑" },
]

/**
 * Shared nav link list. Used both inside the desktop sidebar and the mobile
 * drawer so the two views never drift.
 */
function NavLinks() {
  const { profileType } = useProfile()
  const pathname = usePathname()

  const navItems = BASE_NAV_ITEMS.filter(
    (item) => !item.industry || (profileType !== null && item.industry.includes(profileType))
  )

  return (
    <>
      {profileType === "retail" && (
        <Link
          href="/pos"
          className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-semibold transition-colors ${
            pathname === "/pos"
              ? "bg-primary text-primary-foreground"
              : "bg-emerald-50 text-emerald-700 hover:bg-emerald-100 dark:bg-emerald-950 dark:text-emerald-300 dark:hover:bg-emerald-900"
          }`}
        >
          <span className="text-base" aria-hidden="true">
            🖥️
          </span>
          POS 收银
        </Link>
      )}

      <div className="my-1 border-t border-border" />

      {navItems.map((item) => (
        <Link
          key={item.href}
          href={item.href}
          className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
            pathname?.startsWith(item.href)
              ? "bg-muted font-medium text-foreground"
              : "text-muted-foreground hover:bg-muted hover:text-foreground"
          }`}
        >
          {item.icon && (
            <span className="text-base" aria-hidden="true">
              {item.icon}
            </span>
          )}
          {item.label}
        </Link>
      ))}
    </>
  )
}

/**
 * DashboardSidebar — desktop-only sidebar (md and up). Mobile users get
 * MobileNav instead.
 */
export function DashboardSidebar() {
  return (
    <nav className="hidden w-56 flex-col gap-1 border-r border-border bg-background p-3 md:flex">
      <NavLinks />
    </nav>
  )
}

/**
 * MobileNav — sticky top bar with hamburger + slide-out drawer. Rendered only
 * below md. Closes automatically when the route changes.
 */
export function MobileNav() {
  const [open, setOpen] = useState(false)
  const pathname = usePathname()

  useEffect(() => {
    setOpen(false)
  }, [pathname])

  return (
    <>
      <div className="sticky top-0 z-20 flex h-14 items-center border-b border-border bg-background/95 px-3 backdrop-blur md:hidden">
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-label="打开导航菜单"
          aria-expanded={open}
          className="inline-flex h-10 w-10 items-center justify-center rounded-md hover:bg-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span aria-hidden="true" className="text-xl leading-none">
            ☰
          </span>
        </button>
        <span className="ml-3 text-sm font-medium">Lurus Tally</span>
      </div>

      {open && (
        <>
          <div
            className="fixed inset-0 z-40 bg-black/50 md:hidden"
            onClick={() => setOpen(false)}
            aria-hidden="true"
          />
          <nav
            className="fixed inset-y-0 left-0 z-50 flex w-64 flex-col gap-1 bg-background p-3 shadow-xl md:hidden"
            role="dialog"
            aria-modal="true"
            aria-label="导航菜单"
          >
            <div className="mb-2 flex justify-end">
              <button
                type="button"
                onClick={() => setOpen(false)}
                aria-label="关闭导航菜单"
                className="inline-flex h-8 w-8 items-center justify-center rounded-md hover:bg-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span aria-hidden="true">✕</span>
              </button>
            </div>
            <NavLinks />
          </nav>
        </>
      )}
    </>
  )
}
