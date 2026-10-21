// Shop roles and the permission subset the pages gate on. Pure data and
// functions, so server components can use it as well as the useRole hook.

/** The five shop roles; mirrors internal/domain/team on the backend. */
export type ShopRole = "owner" | "manager" | "clerk" | "cashier" | "finance"

/** Capabilities the pages gate on; mirrors internal/domain/permission. */
export type Perm =
  | "ai.execute"
  | "ai.use"
  | "audit.view"
  | "bill.approve"
  | "bill.cancel"
  | "bill.checkout"
  | "bill.draft"
  | "bill.read"
  | "bill.return"
  | "bill.return.own"
  | "billing.manage"
  | "catalog.write"
  | "cost.view"
  | "customer.write"
  | "export.stock"
  | "invoice.manage"
  | "payment.read"
  | "payment.record"
  | "print.receipt"
  | "print.settings"
  | "product.write"
  | "purchase.view"
  | "replenish.view"
  | "report.view"
  | "settings.manage"
  | "stock.adjust"
  | "team.manage"
  | "token.manage"

const ROLES: readonly ShopRole[] = ["owner", "manager", "clerk", "cashier", "finance"]

/** Mirrors the backend table; roles.test.ts checks it against testdata/matrix.json. */
export const ROLE_PERMS: Record<ShopRole, readonly Perm[]> = {
  owner: [
    "ai.execute", "ai.use", "audit.view", "bill.approve", "bill.cancel", "bill.checkout", "bill.draft", "bill.read", "bill.return", "bill.return.own", "billing.manage", "catalog.write", "cost.view", "customer.write", "export.stock", "invoice.manage", "payment.read", "payment.record", "print.receipt", "print.settings", "product.write", "purchase.view", "replenish.view", "report.view", "settings.manage", "stock.adjust", "team.manage", "token.manage",
  ],
  manager: [
    "ai.execute", "ai.use", "audit.view", "bill.approve", "bill.cancel", "bill.checkout", "bill.draft", "bill.read", "bill.return", "bill.return.own", "catalog.write", "cost.view", "customer.write", "export.stock", "invoice.manage", "payment.read", "payment.record", "print.receipt", "print.settings", "product.write", "purchase.view", "replenish.view", "report.view", "settings.manage", "stock.adjust", "team.manage",
  ],
  clerk: [
    "ai.use", "bill.draft", "bill.read", "catalog.write", "customer.write", "export.stock", "print.receipt", "product.write", "purchase.view", "replenish.view", "stock.adjust",
  ],
  cashier: [
    "bill.checkout", "bill.read", "bill.return.own", "customer.write", "print.receipt",
  ],
  finance: [
    "ai.use", "audit.view", "bill.read", "cost.view", "export.stock", "invoice.manage", "payment.read", "payment.record", "purchase.view", "replenish.view", "report.view",
  ],
}

const LABELS: Record<ShopRole, string> = {
  owner: "老板",
  manager: "店长",
  clerk: "店员",
  cashier: "收银",
  finance: "财务",
}

/**
 * normalizeRole folds whatever the session carries into a shop role. Returns
 * null while the role is not known yet; an unrecognised value gets the least
 * privilege and is never promoted to owner.
 */
export function normalizeRole(role: string | null | undefined, isOwner: boolean): ShopRole | null {
  if (isOwner) return "owner"
  if (!role) return null
  if ((ROLES as readonly string[]).includes(role)) return role as ShopRole
  if (role === "admin") return "manager"
  return "clerk"
}

export function roleCan(role: ShopRole | null, perm: Perm): boolean {
  return role !== null && ROLE_PERMS[role].includes(perm)
}

export function roleLabel(role: string): string {
  return LABELS[role as ShopRole] ?? "店员"
}

const PROFILE_LABELS: Record<string, string> = {
  retail: "零售",
  cross_border: "跨境电商",
  hybrid: "零售加跨境",
  horticulture: "苗木",
}

/** profileTypeLabel is the shop-facing name of the business type. */
export function profileTypeLabel(profileType: string | null | undefined): string {
  if (!profileType) return "未设置"
  return PROFILE_LABELS[profileType] ?? "其他"
}

/** roleLabelFor names the role of someone whose session may carry the owner flag. */
export function roleLabelFor(role: string | null | undefined, isOwner: boolean): string {
  const r = normalizeRole(role, isOwner)
  return r ? LABELS[r] : "—"
}
