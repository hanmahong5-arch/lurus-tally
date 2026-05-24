/**
 * Static definitions for command palette groups.
 *
 * Linear/Raycast style: Pages + Actions are always shown.
 * Recent items are injected at runtime.
 * Entity items are populated dynamically from the search API.
 */

import type { EntityType } from "@/lib/api/search"

export interface PaletteAction {
  id: string
  label: string
  /** Category shown as the group header in the palette. */
  group: "pages" | "actions" | "recent" | "entities"
  /** Optional icon (emoji or lucide name — caller decides). */
  icon?: string
  /** URL to navigate to, or undefined for actions. */
  href?: string
  /** Keyboard shortcut hint shown on the right. */
  shortcut?: string
  /** Sublabel shown below the main label (entity search only). */
  sublabel?: string
  /** Entity type (entity search only). */
  entityType?: EntityType
}

/** Map entity type to a navigation route. Returns undefined when no route exists. */
export function entityHref(type: EntityType, id: string): string | undefined {
  switch (type) {
    case "product":
      return `/products/${id}`
    case "supplier":
      return `/suppliers/${id}`
    case "bill":
      // bill_type is not surfaced in EntityResult; default to /purchases/:id.
      return `/purchases/${id}`
    case "customer":
      // /finance exists; no customer detail route in V1.
      return undefined
    default:
      return undefined
  }
}

/** Static page navigation items. */
export const PAGE_ACTIONS: PaletteAction[] = [
  { id: "nav-dashboard", label: "仪表盘", group: "pages", icon: "📊", href: "/dashboard" },
  { id: "nav-products",  label: "商品管理", group: "pages", icon: "📦", href: "/products" },
  { id: "nav-purchases", label: "采购管理", group: "pages", icon: "🛒", href: "/purchases" },
  { id: "nav-sales",     label: "销售管理", group: "pages", icon: "📈", href: "/sales" },
  { id: "nav-finance",   label: "财务管理", group: "pages", icon: "💰", href: "/finance/exchange-rates" },
  { id: "nav-pos",       label: "POS 收银", group: "pages", icon: "🖥️", href: "/pos" },
  { id: "nav-sub",       label: "订阅与计费", group: "pages", icon: "💳", href: "/subscription" },
]

/** Static quick-action items. */
export const QUICK_ACTIONS: PaletteAction[] = [
  { id: "act-new-purchase", label: "新建采购单", group: "actions", icon: "➕", href: "/purchases/new", shortcut: "" },
  { id: "act-new-sale",     label: "新建销售单", group: "actions", icon: "➕", href: "/sales/new", shortcut: "" },
  { id: "act-new-product",  label: "新建商品",   group: "actions", icon: "➕", href: "/products/new", shortcut: "" },
]

/** AI mode sentinel — shown when the user types >5 chars + Tab. */
export const AI_ASK_ACTION = (query: string): PaletteAction => ({
  id: "ai-ask",
  label: `Ask AI: ${query}`,
  group: "actions",
  icon: "✨",
})
