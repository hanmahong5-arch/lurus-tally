// API client for /api/v1/me and /api/v1/tenant/profile.
//
// Both endpoints require the user's access_token in Authorization header.
// In server components / server actions, retrieve the token from the NextAuth
// session via auth() and pass it to these helpers explicitly.

const BACKEND_URL =
  // Browser: same-origin proxied through Next.js
  typeof window !== "undefined"
    ? ""
    : // Server-side: in-cluster service URL
      process.env.BACKEND_URL ?? "http://tally-backend:18200"

export interface MeResponse {
  user_id: string
  tenant_id: string
  email: string
  display_name: string
  role: string
  is_owner: boolean
  profile_type: "" | "cross_border" | "retail" | "hybrid"
  is_first_time: boolean
}

export type ProfileType = "cross_border" | "retail"

export interface TenantProfile {
  id: string
  tenant_id: string
  profile_type: ProfileType
  inventory_method: string
  created_at: string
  updated_at: string
}

export class TallyApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly detail?: string,
  ) {
    super(message)
    this.name = "TallyApiError"
  }
}

async function request<T>(
  path: string,
  init: RequestInit = {},
  accessToken?: string,
): Promise<T> {
  const headers = new Headers(init.headers)
  if (accessToken) headers.set("Authorization", `Bearer ${accessToken}`)
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }

  const res = await fetch(`${BACKEND_URL}${path}`, {
    ...init,
    headers,
    cache: "no-store",
  })
  if (!res.ok) {
    let detail: string | undefined
    try {
      const body = (await res.json()) as { detail?: string; error?: string }
      detail = body.detail ?? body.error
    } catch {
      // Non-JSON body — leave detail undefined
    }
    throw new TallyApiError(
      `Tally API ${path} returned ${res.status}`,
      res.status,
      detail,
    )
  }
  return (await res.json()) as T
}

/** GET /api/v1/me — full user identity payload. */
export async function getMe(accessToken: string): Promise<MeResponse> {
  return request<MeResponse>("/api/v1/me", { method: "GET" }, accessToken)
}

/**
 * POST /api/v1/tenant/profile — choose business type. Idempotent: returns
 * existing profile when called twice with the same profile_type.
 *
 * Throws TallyApiError(409) when a different profile is already set.
 */
export async function chooseProfile(
  accessToken: string,
  profileType: ProfileType,
): Promise<TenantProfile> {
  return request<TenantProfile>(
    "/api/v1/tenant/profile",
    {
      method: "POST",
      body: JSON.stringify({ profile_type: profileType }),
    },
    accessToken,
  )
}
