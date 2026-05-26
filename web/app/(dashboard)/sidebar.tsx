"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { usePathname } from "next/navigation"
import { useProfile } from "@/lib/profile"

import { AccountCard } from "@/components/account/account-card"

interface NavItem {
  href: string
  label: string
  icon?: string
  /** Only shown when the current profileType is in this list. */
  industry?: string[]
  /** Small badge text to the right of the label (e.g. "NEW"). */
  badge?: string
}

interface NavSection {
  /** Lowercase, displayed as a small uppercase header above the items. */
  title?: string
  items: NavItem[]
}

const SECTIONS: NavSection[] = [
  {
    title: "WORKSPACE",
    items: [
      { href: "/dashboard", label: "仪表盘", icon: "📊" },
      { href: "/ai", label: "AI 助手", icon: "🤖", badge: "NEW" },
      { href: "/todo", label: "待办", icon: "🔔" },
    ],
  },
  {
    title: "经营",
    items: [
      { href: "/products", label: "商品", icon: "📦" },
      { href: "/stock", label: "库存", icon: "🏬" },
      { href: "/replenish", label: "补货", icon: "📋" },
      { href: "/purchases", label: "采购", icon: "🛒" },
      { href: "/sales", label: "销售", icon: "📊" },
      { href: "/payments", label: "付款", icon: "💳", badge: "NEW" },
      { href: "/reports", label: "报表", icon: "📈", badge: "NEW" },
    ],
  },
  {
    title: "设置",
    items: [
      { href: "/suppliers", label: "供应商", icon: "🏭" },
      { href: "/warehouses", label: "仓库", icon: "🏪" },
      { href: "/units", label: "单位", icon: "📐", badge: "NEW" },
      { href: "/account?tab=api-keys", label: "API 密钥", icon: "🔑" },
      { href: "/account?tab=subscription", label: "订阅", icon: "💰" },
      { href: "/dictionary", label: "苗木字典", icon: "🌿", industry: ["horticulture"] },
      { href: "/projects", label: "项目", icon: "🏗️", industry: ["horticulture"] },
    ],
  },
]

function filterSections(sections: NavSection[], profileType: string): NavSection[] {
  return sections
    .map((s) => ({
      ...s,
      items: s.items.filter(
        (i) => !i.industry || (profileType !== "" && i.industry.includes(profileType)),
      ),
    }))
    .filter((s) => s.items.length > 0)
}

/**
 * NavLink renders one row in the sidebar. We match on path prefix BUT not on
 * query (so `/account?tab=api-keys` highlights when on `/account`).
 */
function NavLink({ item, active }: { item: NavItem; active: boolean }) {
  return (
    <Link
      href={item.href}
      className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
        active
          ? "bg-muted font-medium text-foreground"
          : "text-muted-foreground hover:bg-muted hover:text-foreground"
      }`}
    >
      {item.icon && (
        <span className="text-base" aria-hidden="true">
          {item.icon}
        </span>
      )}
      <span className="flex-1">{item.label}</span>
      {item.badge && (
        <span className="rounded-full bg-primary/10 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-primary">
          {item.badge}
        </span>
      )}
    </Link>
  )
}

function isActive(pathname: string | null, href: string): boolean {
  if (!pathname) return false
  // For query-string hrefs, compare only the pathname part.
  const target = href.split("?")[0]
  if (target === "/") return pathname === "/"
  return pathname === target || pathname.startsWith(target + "/")
}

/** Shared between desktop sidebar and mobile drawer. */
function NavLinks() {
  const { profileType } = useProfile()
  const pathname = usePathname()
  const sections = filterSections(SECTIONS, profileType ?? "")

  return (
    <>
      {profileType === "retail" && (
        <Link
          href="/pos"
          className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-semibold transition-colors ${
            pathname === "/pos"
              ? "bg-primary text-primary-foreground"
              : "bg-primary/10 text-primary hover:bg-primary/20"
          }`}
        >
          <span className="text-base" aria-hidden="true">
            🖥️
          </span>
          POS 收银
        </Link>
      )}

      <nav className="flex flex-col gap-3">
        {sections.map((section, idx) => (
          <div key={section.title ?? idx} className="flex flex-col gap-0.5">
            {section.title && (
              <div className="px-3 pt-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70">
                {section.title}
              </div>
            )}
            {section.items.map((item) => (
              <NavLink key={item.href} item={item} active={isActive(pathname, item.href)} />
            ))}
          </div>
        ))}
      </nav>
    </>
  )
}

/**
 * DashboardSidebar — desktop-only sidebar (md and up). Layout is:
 *   POS (retail only) → grouped nav (workspace / business / settings) → spacer
 *   → AccountCard pinned at bottom.
 */
export function DashboardSidebar() {
  return (
    <aside className="hidden w-56 flex-col gap-3 border-r border-border bg-background p-3 md:flex">
      <NavLinks />
      <AccountCard />
    </aside>
  )
}

/**
 * MobileNav — sticky top bar with hamburger + slide-out drawer. Closes on
 * route change. Includes the account card at the bottom for parity with
 * desktop.
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
          <aside
            className="fixed inset-y-0 left-0 z-50 flex w-64 flex-col gap-3 bg-background p-3 shadow-xl md:hidden"
            role="dialog"
            aria-modal="true"
            aria-label="导航菜单"
          >
            <div className="mb-1 flex justify-end">
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
            <AccountCard />
          </aside>
        </>
      )}
    </>
  )
}
