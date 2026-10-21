"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { usePathname } from "next/navigation"
import {
  LayoutDashboardIcon,
  SparklesIcon,
  BellIcon,
  PackageIcon,
  WarehouseIcon,
  ClipboardListIcon,
  ShoppingCartIcon,
  TrendingUpIcon,
  CreditCardIcon,
  ReceiptIcon,
  LineChartIcon,
  FactoryIcon,
  UsersIcon,
  StoreIcon,
  RulerIcon,
  ShoppingBagIcon,
  KeyRoundIcon,
  WalletIcon,
  LeafIcon,
  FolderKanbanIcon,
  PrinterIcon,
  MonitorIcon,
  BookOpenIcon,
  LifeBuoyIcon,
  UserPlusIcon,
  HistoryIcon,
  type LucideIcon,
} from "lucide-react"
import { useProfile } from "@/lib/profile"
import { useRole, type Perm } from "@/hooks/use-role"
import { getTodoSummary } from "@/lib/api/todo"
import { CashierModeControl, CashierModeRedirect, useCashierMode } from "@/components/cashier-mode-gate"

import { AccountCard } from "@/components/account/account-card"

interface NavItem {
  href: string
  label: string
  icon?: LucideIcon
  /** Only shown when the current profileType is in this list. */
  industry?: string[]
  /** Hidden until the backend reports the AI assistant is available. */
  needsAI?: boolean
  /** For these profiles the item is tucked under the collapsed 高级 group. */
  advancedFor?: string[]
  /** Hidden from people whose shop role lacks this permission. */
  perm?: Perm
  /** Shown when the role holds any one of these (instead of perm). */
  anyPerm?: Perm[]
  /** Pending-item count shown as a red dot with a number; hidden when 0. */
  count?: number
}

interface NavSection {
  /** Lowercase, displayed as a small uppercase header above the items. */
  title?: string
  items: NavItem[]
  /** Rendered as a collapsed <details> so rarely-needed entries stay out of the way. */
  collapsible?: boolean
}

/** Shops that sell in person: Shopify and API keys are not part of their day. */
const SIMPLE_PROFILES = ["retail", "horticulture"]

const SECTIONS: NavSection[] = [
  {
    title: "工作台",
    items: [
      { href: "/dashboard", label: "仪表盘", icon: LayoutDashboardIcon },
      { href: "/ai", label: "AI 助手", icon: SparklesIcon, needsAI: true, perm: "ai.use" },
      { href: "/todo", label: "待办", icon: BellIcon },
    ],
  },
  {
    title: "经营",
    items: [
      { href: "/products", label: "商品", icon: PackageIcon },
      { href: "/stock", label: "库存", icon: WarehouseIcon },
      { href: "/replenish", label: "补货", icon: ClipboardListIcon, perm: "replenish.view" },
      { href: "/purchases", label: "采购", icon: ShoppingCartIcon, anyPerm: ["cost.view", "bill.draft"] },
      { href: "/sales", label: "销售", icon: TrendingUpIcon },
      { href: "/payments", label: "收付款", icon: CreditCardIcon, perm: "payment.read" },
      { href: "/invoices", label: "发票", icon: ReceiptIcon, perm: "payment.read" },
      { href: "/reports", label: "报表", icon: LineChartIcon, perm: "report.view" },
    ],
  },
  {
    title: "设置",
    items: [
      { href: "/suppliers", label: "供应商", icon: FactoryIcon },
      { href: "/customers", label: "客户", icon: UsersIcon },
      { href: "/warehouses", label: "仓库", icon: StoreIcon },
      { href: "/units", label: "单位", icon: RulerIcon },
      { href: "/settings/print", label: "打印设置", icon: PrinterIcon, perm: "settings.manage" },
      { href: "/settings/shopify", label: "Shopify 店铺", icon: ShoppingBagIcon, perm: "settings.manage", advancedFor: SIMPLE_PROFILES },
      { href: "/account?tab=api-keys", label: "接口密钥", icon: KeyRoundIcon, perm: "token.manage", advancedFor: SIMPLE_PROFILES },
      { href: "/settings/audit", label: "操作记录", icon: HistoryIcon, perm: "audit.view" },
      { href: "/account?tab=team", label: "团队成员", icon: UserPlusIcon, perm: "team.manage" },
      { href: "/account?tab=subscription", label: "订阅", icon: WalletIcon, perm: "billing.manage" },
      { href: "/dictionary", label: "苗木字典", icon: LeafIcon, industry: ["horticulture"] },
      { href: "/projects", label: "项目", icon: FolderKanbanIcon, industry: ["horticulture"] },
    ],
  },
]

function filterSections(
  sections: NavSection[],
  profileType: string,
  aiEnabled: boolean,
  allowed: (perm: Perm) => boolean = () => true,
): NavSection[] {
  const visible = (i: NavItem) =>
    (!i.industry || (profileType !== "" && i.industry.includes(profileType))) &&
    (!i.needsAI || aiEnabled) &&
    (!i.perm || allowed(i.perm)) &&
    (!i.anyPerm || i.anyPerm.some(allowed))
  const tucked = (i: NavItem) => !!i.advancedFor?.includes(profileType)

  const main = sections
    .map((s) => ({ ...s, items: s.items.filter((i) => visible(i) && !tucked(i)) }))
    .filter((s) => s.items.length > 0)
  const advanced = sections.flatMap((s) => s.items.filter((i) => visible(i) && tucked(i)))
  return advanced.length > 0 ? [...main, { title: "高级", items: advanced, collapsible: true }] : main
}

/**
 * NavLink renders one row in the sidebar. We match on path prefix BUT not on
 * query (so `/account?tab=api-keys` highlights when on `/account`).
 */
function NavLink({ item, active, count }: { item: NavItem; active: boolean; count?: number }) {
  const Icon = item.icon
  return (
    <Link
      href={item.href}
      className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
        active
          ? "bg-muted font-medium text-foreground"
          : "text-muted-foreground hover:bg-muted hover:text-foreground"
      }`}
    >
      {Icon && <Icon className="h-4 w-4 shrink-0" aria-hidden="true" />}
      <span className="flex-1">{item.label}</span>
      {count !== undefined && count > 0 && (
        <span
          className="min-w-[1.25rem] rounded-full bg-destructive px-1.5 py-0.5 text-center text-[10px] font-semibold leading-none text-white"
          aria-label={`${count} 项待办`}
        >
          {count > 99 ? "99+" : count}
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

/**
 * Pending-item total for the 待办 badge, plus whether the AI assistant is
 * available (its entry stays hidden otherwise). Re-read on every route change so the
 * number follows what the user just approved / created. A failed read keeps the
 * last known number instead of flashing a wrong one.
 */
function useTodoSummary(pathname: string | null): { count: number; aiEnabled: boolean } {
  const [state, setState] = useState({ count: 0, aiEnabled: false })
  useEffect(() => {
    const ctrl = new AbortController()
    getTodoSummary({ signal: ctrl.signal })
      .then((s) => {
        if (ctrl.signal.aborted || !s) return
        setState((prev) => ({
          count: typeof s.total === "number" ? s.total : prev.count,
          aiEnabled: s.ai_enabled === true,
        }))
      })
      .catch(() => {})
    return () => ctrl.abort()
  }, [pathname])
  return state
}

/** Shared between desktop sidebar and mobile drawer. */
function NavLinks() {
  const { profileType } = useProfile()
  const pathname = usePathname()
  const { count: todoCount, aiEnabled } = useTodoSummary(pathname)
  const { enabled: cashierMode } = useCashierMode(true)
  // Until the session has told us the role nothing is hidden (the backend still decides).
  const { known, can } = useRole()
  const sections = filterSections(SECTIONS, profileType ?? "", aiEnabled, (perm) => !known || can(perm))

  // Not read back yet: show nothing rather than flash the full menu to a cashier.
  if (cashierMode === null) return null

  // Cashier mode: only the POS and today's records, nothing else to look at.
  if (cashierMode) {
    return (
      <nav className="flex flex-col gap-0.5">
        <Link
          href="/pos"
          className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-semibold transition-colors ${
            pathname === "/pos"
              ? "bg-primary text-primary-foreground"
              : "bg-primary/10 text-primary hover:bg-primary/20"
          }`}
        >
          <MonitorIcon className="h-4 w-4 shrink-0" aria-hidden="true" />
          POS 收银
        </Link>
        <NavLink
          item={{ href: "/pos/history", label: "今日结单", icon: ClipboardListIcon }}
          active={isActive(pathname, "/pos/history")}
        />
        <CashierModeControl variant="exit" />
      </nav>
    )
  }

  return (
    <>
      {profileType === "retail" && (!known || can("bill.checkout")) && (
        <Link
          href="/pos"
          className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-semibold transition-colors ${
            pathname === "/pos"
              ? "bg-primary text-primary-foreground"
              : "bg-primary/10 text-primary hover:bg-primary/20"
          }`}
        >
          <MonitorIcon className="h-4 w-4 shrink-0" aria-hidden="true" />
          POS 收银
        </Link>
      )}

      <nav className="flex flex-col gap-3">
        {sections.map((section, idx) => {
          const links = section.items.map((item) => (
            <NavLink
              key={item.href}
              item={item}
              active={isActive(pathname, item.href)}
              count={item.href === "/todo" ? todoCount : undefined}
            />
          ))
          if (section.collapsible) {
            return (
              <details
                key={section.title ?? idx}
                open={section.items.some((i) => isActive(pathname, i.href))}
                className="flex flex-col gap-0.5"
              >
                <summary className="cursor-pointer list-none px-3 pt-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70 hover:text-muted-foreground">
                  {section.title} ▸
                </summary>
                <div className="mt-0.5 flex flex-col gap-0.5">{links}</div>
              </details>
            )
          }
          return (
            <div key={section.title ?? idx} className="flex flex-col gap-0.5">
              {section.title && (
                <div className="px-3 pt-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70">
                  {section.title}
                </div>
              )}
              {links}
            </div>
          )
        })}
      </nav>
    </>
  )
}

/**
 * SupportLinks — help footer under the AccountCard. Deliberately the quietest
 * thing in the sidebar (11px, muted, no icons larger than the text): it must be
 * findable when a user is stuck and invisible the rest of the time.
 *
 * Both targets are external, so plain <a> rather than next/link.
 * `支持` uses support@lurus.cn, matching the existing www-next contact
 * (src/app/pricing/faq.tsx).
 */
function SupportLinks() {
  const linkCls =
    "inline-flex items-center gap-1 rounded-sm transition-colors hover:text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
  return (
    <div className="flex items-center gap-3 px-3 pb-0.5 text-[11px] text-muted-foreground">
      <a href="https://docs.lurus.cn" target="_blank" rel="noreferrer" className={linkCls}>
        <BookOpenIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
        文档
      </a>
      <a href="mailto:support@lurus.cn" className={linkCls}>
        <LifeBuoyIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
        支持
      </a>
    </div>
  )
}

/**
 * DashboardSidebar — desktop-only sidebar (md and up). Layout is:
 *   POS (retail only) → grouped nav (workspace / business / settings) → spacer
 *   → AccountCard + support links pinned at bottom.
 */
export function DashboardSidebar() {
  return (
    <aside className="hidden w-56 flex-col gap-3 border-r border-border bg-background p-3 md:flex">
      <CashierModeRedirect />
      <NavLinks />
      <AccountCard />
      <SupportLinks />
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
            <SupportLinks />
          </aside>
        </>
      )}
    </>
  )
}
