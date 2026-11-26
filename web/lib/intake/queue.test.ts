import { describe, expect, it } from "vitest"
import { ApiError, NetworkError } from "@/lib/api/errors"
import { DEVICE_ID_KEY, INTAKE_SEQ_KEY } from "@/lib/device-keys"
import { classifyIntake, intakeDuplicateHref, nextIntakeRef, shouldQueueIntake } from "./queue"

describe("intake queue", () => {
  it("numbers intake separately from the device id it stores", () => {
    const mem = new Map<string, string>([[DEVICE_ID_KEY, "abcd1234-rest"]])
    const store = {
      get: (k: string) => mem.get(k) ?? null,
      set: (k: string, v: string) => { mem.set(k, v) },
    }
    expect(nextIntakeRef(store)).toBe("abcd1234-1")
    expect(nextIntakeRef(store)).toBe("abcd1234-2")
    expect(mem.get(INTAKE_SEQ_KEY)).toBe("2")
  })

  it("treats a replay 409 as done and a lost reply as retry", () => {
    const dup = classifyIntake(new ApiError(409, "duplicate_document", "已收过", {
      error: "duplicate_document", message: "这张票已在2026年3月15日收过", existing_id: "row-1", existing_kind: "invoice", invoice_no: "24317000000012345678",
    }))
    expect(dup.send).toBe("done")
    expect(dup.existingId).toBe("row-1")
    expect(dup.existingKind).toBe("invoice")
    expect(dup.existingNo).toBe("24317000000012345678")
    expect(intakeDuplicateHref("invoice", "row-1", "24317000000012345678")).toBe("/invoices?q=24317000000012345678")
    expect(intakeDuplicateHref("intake", "row-2")).toBe("/intake?id=row-2")
    const again = classifyIntake(new ApiError(409, "intake_already_exists", "已上传", { error: "intake_already_exists", existing_id: "row-2" }))
    expect(again.send).toBe("done")
    expect(classifyIntake(new ApiError(422, "money_mismatch", "对不齐", { message: "对不齐" })).send).toBe("failed")
    expect(classifyIntake(new NetworkError("network", "dropped", true)).send).toBe("retry")
    const offline = new NetworkError("offline", "offline", false)
    expect(shouldQueueIntake(offline)).toBe(true)
    expect(shouldQueueIntake(new NetworkError("network", "lost", true))).toBe(false)
  })
})
