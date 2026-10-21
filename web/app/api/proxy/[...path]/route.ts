import { auth } from "@/auth"
import { NextRequest, NextResponse } from "next/server"

const BACKEND_URL = process.env.BACKEND_URL ?? "http://tally-backend:18200"

// Catch-all proxy: forwards browser requests under /api/proxy/* to the Tally
// backend at /api/v1/*, injecting the user's id_token (NextAuth session
// accessToken) as a Bearer header. This lets client components fetch backend
// data without exposing the token to the browser or wiring useSession into
// every API client.
// Dev/E2E offline fallback. The offline Credentials provider (auth.ts) issues
// sessions WITHOUT a backend bearer; the backend in TALLY_DEV_MODE trusts
// X-IDP-Subject / X-Tenant-ID headers instead of a JWT. Mirror that contract
// here so a fully-local stack (dev web + TALLY_DEV_MODE backend) works
// end-to-end. Same double gate as devProviderEnabled() in auth.ts: explicit
// AUTH_DEV_PROVIDER opt-in AND a production hard-block.
function devHeaderFallbackEnabled(): boolean {
  return process.env.AUTH_DEV_PROVIDER === "true" && process.env.NODE_ENV !== "production"
}

async function handle(req: NextRequest, ctx: { params: Promise<{ path: string[] }> }) {
  const session = await auth()
  const devTenantId = devHeaderFallbackEnabled() ? session?.user?.tenantId : undefined
  if (!session?.accessToken && !devTenantId) {
    return NextResponse.json({ error: "unauthorized", detail: "no session" }, { status: 401 })
  }

  const { path } = await ctx.params
  const subPath = path.join("/")
  const search = req.nextUrl.search
  const url = `${BACKEND_URL}/api/v1/${subPath}${search}`

  const headers = new Headers()
  if (session?.accessToken) {
    headers.set("Authorization", `Bearer ${session.accessToken}`)
  } else if (devTenantId) {
    // Offline dev session: speak the TALLY_DEV_MODE header contract.
    headers.set("X-Tenant-ID", devTenantId)
    headers.set("X-IDP-Subject", session?.user?.id || "dev-user")
  }
  const ct = req.headers.get("content-type")
  if (ct) headers.set("Content-Type", ct)
  const ik = req.headers.get("idempotency-key")
  if (ik) headers.set("Idempotency-Key", ik)

  const init: RequestInit = {
    method: req.method,
    headers,
    cache: "no-store",
  }
  if (!["GET", "HEAD"].includes(req.method)) {
    init.body = await req.text()
  }

  let upstream: Response
  try {
    upstream = await fetch(url, init)
  } catch {
    // Backend unreachable / connection dropped: a recognisable 502 lets the
    // browser treat it as a lost response (keep its Idempotency-Key).
    return NextResponse.json({ error: "upstream_unreachable" }, { status: 502 })
  }
  const respHeaders = new Headers()
  const upstreamCT = upstream.headers.get("content-type")
  if (upstreamCT) respHeaders.set("content-type", upstreamCT)
  // Keep the download filename chosen by the backend (exports/*.csv).
  const upstreamCD = upstream.headers.get("content-disposition")
  if (upstreamCD) respHeaders.set("content-disposition", upstreamCD)

  // Pass the backend request id through so the browser can attach it to
  // web_error reports and support can correlate with backend logs.
  const upstreamRID = upstream.headers.get("x-request-id")
  if (upstreamRID) respHeaders.set("x-request-id", upstreamRID)

  return new NextResponse(upstream.body, {
    status: upstream.status,
    headers: respHeaders,
  })
}

export const GET = handle
export const POST = handle
export const PUT = handle
export const PATCH = handle
export const DELETE = handle
