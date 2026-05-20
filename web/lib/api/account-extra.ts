/**
 * Account-center API helpers beyond the summary fan-out in account.ts.
 *
 * Covers the Phase 3 backend additions: sessions, audit log, editable
 * profile, avatar upload.
 */

import { apiFetch } from "./client"

export interface AccountSession {
  id: string
  user_agent: string
  ip_addr?: string
  created_at: string
  last_active: string
  current: boolean
  revoked_at?: string | null
}

export interface ListSessionsResponse {
  items: AccountSession[]
}

export interface AuditEntry {
  id: string
  actor_id: string
  action: string
  target_kind?: string
  target_id?: string
  /** Server returns the payload as a JSON-encoded string so big blobs don't
   *  bloat the response. UI parses on demand. */
  payload: string
  created_at: string
}

export interface ListAuditLogResponse {
  items: AuditEntry[]
  total: number
  limit: number
  offset: number
}

export interface ProfileResponse {
  display_name: string
  phone: string
  avatar_url: string
  updated_at: string
}

export async function listSessions(signal?: AbortSignal): Promise<ListSessionsResponse> {
  return apiFetch<ListSessionsResponse>("/account/sessions", { signal })
}

export async function revokeSession(id: string): Promise<void> {
  await apiFetch<void>(`/account/sessions/${id}`, { method: "DELETE" })
}

export async function listAuditLog(
  limit = 50,
  offset = 0,
  signal?: AbortSignal,
): Promise<ListAuditLogResponse> {
  const usp = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  return apiFetch<ListAuditLogResponse>(`/account/audit-log?${usp.toString()}`, { signal })
}

export async function getProfile(signal?: AbortSignal): Promise<ProfileResponse> {
  return apiFetch<ProfileResponse>("/account/profile", { signal })
}

export async function updateProfile(displayName: string, phone: string): Promise<void> {
  await apiFetch<void>("/account/profile", {
    method: "PUT",
    body: JSON.stringify({ display_name: displayName, phone }),
  })
}

export async function uploadAvatar(file: File): Promise<{ avatar_url: string }> {
  const form = new FormData()
  form.append("file", file)
  // Multipart fetch bypasses apiFetch's JSON helpers; do it manually so the
  // browser sets the boundary header.
  const res = await fetch("/api/proxy/account/avatar", {
    method: "POST",
    body: form,
    cache: "no-store",
  })
  if (!res.ok) {
    let detail = res.statusText
    try {
      const body = (await res.json()) as { detail?: string; error?: string }
      detail = body.detail ?? body.error ?? detail
    } catch {
      // ignore — keep statusText
    }
    throw new Error(`upload avatar: ${res.status} ${detail}`)
  }
  return (await res.json()) as { avatar_url: string }
}
