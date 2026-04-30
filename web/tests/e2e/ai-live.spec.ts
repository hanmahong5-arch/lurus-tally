import { test, expect } from "@playwright/test"

// Real LLM round-trip via /api/v1/ai/chat (SSE). Verifies that AI is wired
// end-to-end: NEWAPI_API_KEY → llmclient → DeepSeek → tools dispatched →
// response chunks streamed back. We expect at least one `chunk` event with
// non-empty content, and a `done` event to close.
test("AI chat returns non-empty response via newapi", async ({ request }) => {
  const sessionRes = await request.get("/api/auth/session")
  const session = await sessionRes.json()
  expect(session.accessToken).toBeTruthy()

  const res = await request.post("/api/v1/ai/chat", {
    headers: {
      Authorization: `Bearer ${session.accessToken}`,
      "Content-Type": "application/json",
    },
    data: { message: "请用中文回复一句：你好" },
  })
  expect(res.ok(), `chat status=${res.status()}`).toBe(true)

  const body = await res.text()
  // SSE body has `event: chunk\ndata: {...}\n\n` repeated until `event: done`.
  expect(body, "expect at least one chunk event").toMatch(/event:\s*chunk/)
  expect(body, "expect a done event").toMatch(/event:\s*done/)
  expect(body, "no error event").not.toMatch(/event:\s*error/)
  // Pull all `data:` lines under `chunk` events and assert at least one has
  // content. We don't assert specific Chinese text — model output varies.
  const chunkContents = [...body.matchAll(/event:\s*chunk\s*\n\s*data:\s*(.+)/g)]
    .map((m) => m[1])
    .filter((s) => {
      try {
        return JSON.parse(s).content?.length > 0
      } catch {
        return false
      }
    })
  expect(chunkContents.length, "non-empty chunk content").toBeGreaterThan(0)
})

test("AI chat rejects missing tenant_id with 401", async ({ request }) => {
  // Same endpoint but no Authorization header → tenant_id middleware blocks.
  const res = await request.post("/api/v1/ai/chat", {
    headers: { "Content-Type": "application/json" },
    data: { message: "ping" },
  })
  expect(res.status()).toBe(401)
})
