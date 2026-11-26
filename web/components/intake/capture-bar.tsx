"use client"

import { useCallback, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { WeChatOpenHint } from "@/components/wechat-open-hint"
import { captureIntake, type CapturePayload } from "@/lib/api/intake"
import { draftGet, draftSet } from "@/lib/draft/idb-storage"
import { classifyIntake, intakeDuplicateHref, nextIntakeRef, shouldQueueIntake } from "@/lib/intake/queue"
import { readTaxQR } from "@/lib/intake/read-qr"
import { QrCamera, type Shot } from "./qr-camera"
import { VoiceCapture } from "./voice-capture"

const QUEUE_KEY = "intake.queue"

function bytesToBase64(bytes: Uint8Array): string {
  let bin = ""
  for (let i = 0; i < bytes.length; i += 0x8000) {
    bin += String.fromCharCode(...Array.from(bytes.subarray(i, i + 0x8000)))
  }
  return btoa(bin)
}

interface Job {
  client_ref: string
  idempotency_key: string
  body: CapturePayload
}

async function readQueue(): Promise<Job[]> {
  return (await draftGet<Job[]>(QUEUE_KEY)) ?? []
}

/** Capture only: shoot, attach a file, or paste a QR. Review happens on a computer. */
export function CaptureBar({ onSent, onExisting }: { onSent?: (id: string) => void; onExisting?: (id: string, kind: string) => void }) {
  const [note, setNote] = useState("")
  const [hit, setHit] = useState<{ id: string; kind: string; number: string } | null>(null)
  const [qr, setQr] = useState("")
  const [direction, setDirection] = useState<"in" | "out">("in")
  const [waiting, setWaiting] = useState(0)

  const remember = useCallback(async (job: Job) => {
    const rows = await readQueue()
    rows.push(job)
    await draftSet(QUEUE_KEY, rows)
    setWaiting(rows.length)
  }, [])

  const deliver = useCallback(async (body: CapturePayload, key: string, queued: boolean): Promise<"keep" | "drop"> => {
    try {
      const doc = await captureIntake(body, key)
      setNote("已送出，等核对")
      setHit(null)
      onSent?.(doc.id)
      return "drop"
    } catch (err) {
      const outcome = classifyIntake(err)
      if (!queued && shouldQueueIntake(err)) {
        await remember({ client_ref: body.client_ref, idempotency_key: key, body })
        setNote(outcome.message)
        return "keep"
      }
      setNote(outcome.message)
      if (outcome.existingId) {
        const kind = outcome.existingKind || "intake"
        setHit({ id: outcome.existingId, kind, number: outcome.existingNo || "" })
        onExisting?.(outcome.existingId, kind)
      } else {
        setHit(null)
      }
      return outcome.send === "retry" && queued ? "keep" : "drop"
    }
  }, [onExisting, onSent, remember])

  const flush = useCallback(async () => {
    try {
      const rows = await readQueue()
      const left: Job[] = []
      for (const job of rows) {
        if (await deliver(job.body, job.idempotency_key, true) === "keep") left.push(job)
      }
      await draftSet(QUEUE_KEY, left)
      setWaiting(left.length)
    } catch {
      setWaiting(0)
    }
  }, [deliver])

  useEffect(() => {
    void flush()
    const on = () => { void flush() }
    window.addEventListener("online", on)
    return () => window.removeEventListener("online", on)
  }, [flush])

  const sendNew = useCallback((body: Omit<CapturePayload, "client_ref">) => {
    const store = window.localStorage
    const client_ref = nextIntakeRef({ get: (k) => store.getItem(k), set: (k, v) => store.setItem(k, v) })
    const key = crypto.randomUUID()
    void deliver({ ...body, direction, client_ref }, key, false)
  }, [deliver, direction])

  const onShot = useCallback((shot: Shot) => {
    sendNew({ source: "camera", qr_payload: shot.qr, content_type: shot.contentType, file_base64: shot.fileBase64 })
  }, [sendNew])

  const onFile = async (file: File | undefined, source: "file" | "voice" | "receipt") => {
    if (!file) return
    if (file.size > 6 * 1024 * 1024) {
      setNote("文件超过 6MB")
      return
    }
    const data = await file.arrayBuffer()
    const qrPayload = source === "file" && file.type.startsWith("image/") ? await readTaxQR(file).catch(() => "") : ""
    sendNew({
      source,
      qr_payload: qrPayload || undefined,
      content_type: file.type || "application/octet-stream",
      file_base64: bytesToBase64(new Uint8Array(data)),
    })
  }

  return (
    <div className="mb-6 space-y-3">
      <WeChatOpenHint />
      <div className="flex flex-wrap items-center gap-2">
        <select aria-label="收件方向" className="rounded border px-2 py-1 text-sm" value={direction} onChange={(e) => setDirection(e.target.value === "out" ? "out" : "in")}>
          <option value="in">进项</option>
          <option value="out">销项</option>
        </select>
        <QrCamera onShot={onShot} />
        <label className="text-sm">
          上传文件
          <input className="ml-2 text-sm" type="file" accept="image/*,application/pdf,.ofd" onChange={(e) => { void onFile(e.target.files?.[0], "file"); e.target.value = "" }} />
        </label>
        <VoiceCapture onAudio={(file) => { void onFile(file, "voice") }} />
        <label className="text-sm">
          语音文件
          <input className="ml-2 text-sm" type="file" accept="audio/*" onChange={(e) => { void onFile(e.target.files?.[0], "voice"); e.target.value = "" }} />
        </label>
        <label className="text-sm">
          回单
          <input className="ml-2 text-sm" type="file" accept="image/*" onChange={(e) => { void onFile(e.target.files?.[0], "receipt"); e.target.value = "" }} />
        </label>
      </div>
      <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); if (qr.trim()) { sendNew({ source: "file", qr_payload: qr.trim() }); setQr("") } }}>
        <Input value={qr} onChange={(e) => setQr(e.target.value)} placeholder="扫码枪或粘贴二维码，回车送出" aria-label="二维码内容" />
        <Button type="submit" variant="outline">送出</Button>
      </form>
      {waiting > 0 && <p className="text-sm">本机还有 {waiting} 张没送出，网络恢复后会自动再传。</p>}
      {note && <p className="text-sm">{note}</p>}
      {hit && <a className="text-sm underline" href={intakeDuplicateHref(hit.kind, hit.id, hit.number)}>{hit.kind === "invoice" ? "打开这张发票" : "打开已有收件"}</a>}
    </div>
  )
}
