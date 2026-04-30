"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useProfile } from "@/lib/profile"

interface NavItem {
  href: string
  label: string
  icon?: string
}

const BASE_NAV_ITEMS: NavItem[] = [
  { href: "/products", label: "商品管理", icon: "📦" },
  { href: "/purchases", label: "采购管理", icon: "🛒" },
  { href: "/sales", label: "销售管理", icon: "📊" },
  { href: "/finance/exchange-rates", label: "财务管理", icon: "💰" },
  { href: "/subscription", label: "订阅与计费", icon: "💳" },
]

/**
 * DashboardSidebar is the client-side navigation sidebar for the dashboard layout.
 * It shows profile-aware nav items — retail users see the POS link.
 */
export function DashboardSidebar() {
  const { profileType } = useProfile()
  const pathname = usePathname()

  const navItems = [...BASE_NAV_ITEMS]

  return (
    <nav className="flex w-56 flex-col gap-1 border-r border-border bg-background p-3">
      {/* POS shortcut — retail only */}
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

      {/* Standard nav items */}
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
    </nav>
  )
}
