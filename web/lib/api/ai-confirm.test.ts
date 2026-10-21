import { describe, it, expect, vi, afterEach } from "vitest"

// confirmPlan must carry the ticked flag indexes, otherwise the server refuses
// every draft the checks flagged.
describe("confirmPlan request body", () => {
  const originalFetch = global.fetch

  afterEach(() => {
    global.fetch = originalFetch
  })

  const okFetch = () =>
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ plan_id: "plan-1", status: "confirmed", type: "create_purchase_draft" }),
    } as Response)

  it("sends {acknowledged_flags:[...]} when lines were ticked", async () => {
    global.fetch = okFetch()
    const { confirmPlan } = await import("./ai")
    await confirmPlan("plan-1", [0, 2])
    const init = vi.mocked(global.fetch).mock.calls[0][1] as RequestInit
    expect(JSON.parse(String(init.body))).toEqual({ acknowledged_flags: [0, 2] })
  })

  it("sends an empty acknowledged_flags list when nothing was flagged", async () => {
    global.fetch = okFetch()
    const { confirmPlan } = await import("./ai")
    await confirmPlan("plan-1")
    const init = vi.mocked(global.fetch).mock.calls[0][1] as RequestInit
    expect(JSON.parse(String(init.body))).toEqual({ acknowledged_flags: [] })
  })
})
