/**
 * Personal Access Token CRUD client.
 * Backend: POST/GET/DELETE /api/v1/auth/pats — see ADR-0011 Phase 2b.
 *
 * The plaintext token is returned ONLY by createPAT, and only that one time.
 * The server never echoes it on subsequent reads — list responses carry the
 * prefix (for identification) but not the secret or its hash.
 */
import { apiFetch } from "./client"

export interface PAT {
  id: string
  name: string
  prefix: string
  scopes: string[]
  created_at: string
  expires_at?: string | null
  last_used_at?: string | null
}

export interface CreatedPAT extends PAT {
  /** Plaintext bearer string. Surface once and never store. */
  token: string
}

export interface CreatePATRequest {
  name: string
  /** ISO timestamp; omit for no expiry. */
  expires_at?: string
}

export async function createPAT(req: CreatePATRequest, tenantId?: string): Promise<CreatedPAT> {
  return apiFetch<CreatedPAT>("/auth/pats", {
    method: "POST",
    body: JSON.stringify(req),
    tenantId,
  })
}

export async function listPATs(tenantId?: string, signal?: AbortSignal): Promise<PAT[]> {
  const res = await apiFetch<{ items: PAT[] }>("/auth/pats", { tenantId, signal, retry: 2 })
  return res.items ?? []
}

export async function revokePAT(id: string, tenantId?: string): Promise<void> {
  await apiFetch<void>(`/auth/pats/${id}`, { method: "DELETE", tenantId })
}
