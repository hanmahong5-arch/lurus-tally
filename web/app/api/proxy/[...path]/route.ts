import { auth } from "@/auth"
import { NextRequest, NextResponse } from "next/server"

const BACKEND_URL = process.env.BACKEND_URL ?? "http://tally-backend:18200"

// Catch-all proxy: forwards browser requests under /api/proxy/* to the Tally
// backend at /api/v1/*, injecting the user's id_token (NextAuth session
// accessToken) as a Bearer header. This lets client components fetch backend
// data without exposing the token to the browser or wiring useSession into
// every API client.
async function handle(req: NextRequest, ctx: { params: Promise<{ path: string[] }> }) {
  const session = await auth()
  if (!session?.accessToken) {
    return NextResponse.json({ error: "unauthorized", detail: "no session" }, { status: 401 })
  }

  const { path } = await ctx.params
  const subPath = path.join("/")
  const search = req.nextUrl.search
  const url = `${BACKEND_URL}/api/v1/${subPath}${search}`

  const headers = new Headers()
  headers.set("Authorization", `Bearer ${session.accessToken}`)
  const ct = req.headers.get("content-type")
  if (ct) headers.set("Content-Type", ct)

  const init: RequestInit = {
    method: req.method,
    headers,
    cache: "no-store",
  }
  if (!["GET", "HEAD"].includes(req.method)) {
    init.body = await req.text()
  }

  const upstream = await fetch(url, init)
  const respHeaders = new Headers()
  const upstreamCT = upstream.headers.get("content-type")
  if (upstreamCT) respHeaders.set("content-type", upstreamCT)

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
