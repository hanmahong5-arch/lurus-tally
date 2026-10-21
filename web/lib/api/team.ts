import { apiFetch } from "./client"

export type TeamRole = "manager" | "clerk" | "cashier" | "finance"

/** Roles an owner/manager can hand out, in the order shown in pickers. */
export const ASSIGNABLE_ROLES: { value: TeamRole; label: string; hint: string }[] = [
  { value: "manager", label: "店长", hint: "能审核、看成本、管理店员收银财务，不能管付费" },
  { value: "clerk", label: "店员", hint: "管商品和库存、开单草稿，看不到成本毛利，不能审核" },
  { value: "cashier", label: "收银", hint: "收银结账、查价，可退当天自己开的小票" },
  { value: "finance", label: "财务", hint: "管收付款、发票和报表，能看成本毛利，不改库存" },
]

export interface TeamMember {
  account_id: number
  role: string
  role_label: string
  display_name: string
  email_masked: string
  phone_masked: string
  joined_at: string
  is_self: boolean
  /** Gentle suggestion from the server, e.g. a cashier in a shop without a cashier desk. */
  role_hint?: string
}

export interface TeamInvite {
  id: string
  role: string
  role_label: string
  expires_at: string
  created_at: string
}

export interface CreatedInvite {
  invite_id: string
  /** Site-relative link, e.g. /join?token=... */
  link: string
  role: string
  role_label: string
  expires_at: string
}

export interface JoinResult {
  tenant_id: string
  shop_name: string
  role: string
  role_label: string
}

export interface MemberList {
  members: TeamMember[]
  /** False for shops without a cashier desk (anything but retail): the cashier role is not offered. */
  cashierAllowed: boolean
}

export async function listMembers(opts: { signal?: AbortSignal } = {}): Promise<MemberList> {
  const r = await apiFetch<{ items: TeamMember[]; cashier_allowed?: boolean }>("/team/members", { ...opts, silent: true })
  return { members: r.items ?? [], cashierAllowed: r.cashier_allowed !== false }
}

export function updateMemberRole(
  accountId: number,
  role: TeamRole,
): Promise<{ account_id: number; role: string; role_label: string }> {
  return apiFetch(`/team/members/${accountId}`, {
    method: "PATCH",
    body: JSON.stringify({ role }),
    silent: true,
  })
}

export function removeMember(accountId: number): Promise<void> {
  return apiFetch(`/team/members/${accountId}`, { method: "DELETE", silent: true })
}

export function createInvite(role: TeamRole, ttlHours: number): Promise<CreatedInvite> {
  return apiFetch("/team/invites", {
    method: "POST",
    body: JSON.stringify({ role, ttl_hours: ttlHours }),
    silent: true,
  })
}

export async function listInvites(opts: { signal?: AbortSignal } = {}): Promise<TeamInvite[]> {
  const r = await apiFetch<{ items: TeamInvite[] }>("/team/invites", { ...opts, silent: true })
  return r.items ?? []
}

export function revokeInvite(id: string): Promise<void> {
  return apiFetch(`/team/invites/${encodeURIComponent(id)}`, { method: "DELETE", silent: true })
}

/**
 * Redeems an invite. abandonEmptyShop confirms giving up the caller's own empty
 * shop (sample data only, sole member) first; the server re-checks that.
 */
export function joinShop(
  token: string,
  opts: { abandonEmptyShop?: boolean; inviteId?: string } = {},
): Promise<JoinResult> {
  const body: Record<string, unknown> = { token }
  // The id lets the server tell an invite of the caller's own shop without using it up.
  if (opts.inviteId) body.invite_id = opts.inviteId
  if (opts.abandonEmptyShop) body.abandon_empty_shop = true
  return apiFetch("/team/join", {
    method: "POST",
    body: JSON.stringify(body),
    silent: true,
  })
}
