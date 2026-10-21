import { describe, it, expect, vi, afterEach } from "vitest"
import { NextRequest } from "next/server"

vi.mock("@/auth", () => ({
  auth: vi.fn().mockResolvedValue({ accessToken: "tok", user: { id: "u1" } }),
}))

import { GET, POST } from "./route"

describe("proxy route", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("returns 502 upstream_unreachable and forwards Idempotency-Key", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error("ECONNREFUSED"))
    vi.stubGlobal("fetch", fetchMock)
    const req = new NextRequest("http://localhost/api/proxy/purchase-bills", {
      method: "POST",
      headers: { "content-type": "application/json", "idempotency-key": "key-1" },
      body: "{}",
    })
    const res = await POST(req, { params: Promise.resolve({ path: ["purchase-bills"] }) })
    expect(res.status).toBe(502)
    expect(await res.json()).toEqual({ error: "upstream_unreachable" })
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect((init.headers as Headers).get("Idempotency-Key")).toBe("key-1")
  })

  it("passes Content-Disposition through so export filenames survive", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("a,b", {
        status: 200,
        headers: {
          "content-type": "text/csv; charset=utf-8",
          "content-disposition": 'attachment; filename="bills-20261010.csv"',
        },
      }),
    )
    vi.stubGlobal("fetch", fetchMock)
    const req = new NextRequest("http://localhost/api/proxy/exports/bills.csv", { method: "GET" })
    const res = await GET(req, { params: Promise.resolve({ path: ["exports", "bills.csv"] }) })
    expect(res.status).toBe(200)
    expect(res.headers.get("content-disposition")).toBe('attachment; filename="bills-20261010.csv"')
  })

  it("passes X-Request-Id through so web_error reports can carry it", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("{}", { status: 500, headers: { "content-type": "application/json", "x-request-id": "req-abc" } }),
    )
    vi.stubGlobal("fetch", fetchMock)
    const req = new NextRequest("http://localhost/api/proxy/bills", { method: "GET" })
    const res = await GET(req, { params: Promise.resolve({ path: ["bills"] }) })
    expect(res.status).toBe(500)
    expect(res.headers.get("x-request-id")).toBe("req-abc")
  })
})
