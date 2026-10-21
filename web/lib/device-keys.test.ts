import { describe, it, expect } from "vitest"
import { SIGNOUT_SIGNAL_KEY, SWITCH_COOKIE } from "./device-keys"

describe("device keys", () => {
  it("keeps the names the sign-in page and the sign-out button must agree on", () => {
    expect(SWITCH_COOKIE).toBe("tally-switch")
    expect(SIGNOUT_SIGNAL_KEY).toBe("tally:signed-out-at")
  })
})
