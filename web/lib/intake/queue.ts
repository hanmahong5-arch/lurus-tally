import { ApiError, NetworkError } from "@/lib/api/errors"
import { DEVICE_ID_KEY, INTAKE_SEQ_KEY } from "@/lib/device-keys"

export type IntakeSend = "done" | "failed" | "retry"

export interface IntakeClass {
  send: IntakeSend
  existingId?: string
  existingKind?: string
  existingNo?: string
  message: string
}

type Store = {
  get: (key: string) => string | null
  set: (key: string, value: string) => void
}

/** client_ref is this device's 8 characters plus its own sequence, not the till's. */
export function nextIntakeRef(store: Store): string {
  let device = store.get(DEVICE_ID_KEY) ?? ""
  if (!device) {
    device = `dev${Math.random().toString(16).slice(2, 10)}`
    store.set(DEVICE_ID_KEY, device)
  }
  const n = (Number(store.get(INTAKE_SEQ_KEY)) || 0) + 1
  store.set(INTAKE_SEQ_KEY, String(n))
  return `${device.replace(/-/g, "").slice(0, 8).padEnd(8, "0")}-${n}`
}

/** A capture that never left the phone can be queued. A 409 means the row already exists. */
export function classifyIntake(err: unknown): IntakeClass {
  if (err instanceof NetworkError) {
    if (!err.sent) return { send: "retry", message: "还没发出去，已留在本机" }
    return { send: "retry", message: "回复丢了，用原编号再送一次" }
  }
  if (err instanceof ApiError) {
    const body = (err.body ?? {}) as { error?: unknown; message?: unknown; existing_id?: unknown; existing_kind?: unknown; invoice_no?: unknown }
    const code = typeof body.error === "string" ? body.error : err.code
    if (err.status === 409 && (code === "duplicate_document" || code === "intake_already_exists")) {
      return {
        send: "done",
        existingId: typeof body.existing_id === "string" ? body.existing_id : undefined,
        existingKind: typeof body.existing_kind === "string" ? body.existing_kind : undefined,
        existingNo: typeof body.invoice_no === "string" ? body.invoice_no : undefined,
        message: typeof body.message === "string" && body.message ? body.message : "这张票已经收过",
      }
    }
    if (err.status >= 500 || err.status === 408 || err.status === 429) {
      return { send: "retry", message: "服务暂时不可用" }
    }
    const message = typeof body.message === "string" && body.message ? body.message : err.message
    return { send: "failed", message: message || "这张没有收下" }
  }
  return { send: "retry", message: err instanceof Error ? err.message : "上传失败" }
}

export function shouldQueueIntake(err: unknown): boolean {
  return err instanceof NetworkError && err.sent === false
}

/** Where a duplicate capture should open: the invoice search, or that inbox row. */
export function intakeDuplicateHref(kind: string | undefined, id: string, number?: string): string {
  if (kind === "invoice") return number ? `/invoices?q=${encodeURIComponent(number)}` : "/invoices"
  return `/intake?id=${encodeURIComponent(id)}`
}
