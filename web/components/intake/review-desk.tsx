"use client"

import { useCallback, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { acceptIntake, getIntake, intakeFile, listIntake, lockIntake, rejectIntake, type IntakeDoc } from "@/lib/api/intake"
import { extractApiError } from "@/lib/api/errors"
import { intakeDuplicateHref } from "@/lib/intake/queue"
import { emptyReview, formFromDoc, reviewReady, ReviewFields, type ReviewForm } from "./review-fields"

/** Desktop review: the original on the left, the fields on the right. Confirm never approves stock or records a payment. */
export function ReviewDesk({ focusId, reloadKey = 0, onDone }: { focusId?: string; reloadKey?: number; onDone?: () => void }) {
  const [items, setItems] = useState<IntakeDoc[]>([])
  const [pinned, setPinned] = useState<IntakeDoc | null>(null)
  const [follow, setFollow] = useState(true)
  const [idx, setIdx] = useState(0)
  const [form, setForm] = useState<ReviewForm>(emptyReview)
  const [bills, setBills] = useState<NonNullable<IntakeDoc["open_bills"]>>([])
  const [src, setSrc] = useState("")
  const [fileType, setFileType] = useState("")
  const [turn, setTurn] = useState(0)
  const [zoom, setZoom] = useState(1)
  const [note, setNote] = useState("")
  const [lockNote, setLockNote] = useState("")

  const load = useCallback(async () => {
    const res = await listIntake()
    setItems((res.items ?? []).filter((d) => d.status === "received" || d.status === "needs_review"))
  }, [])

  useEffect(() => { void load().catch(() => setNote("收件列表没有读出来")) }, [load, reloadKey])
  useEffect(() => { setFollow(true) }, [focusId])

  const inQueue = items.find((d) => d.id === focusId)
  const focused = follow && focusId ? (inQueue ?? (pinned?.id === focusId ? pinned : undefined)) : undefined
  const waiting = follow && !!focusId && !focused
  const doc = focused ?? (waiting ? undefined : items[idx])

  useEffect(() => {
    if (!follow || !focusId || items.some((d) => d.id === focusId)) return
    const ctrl = new AbortController()
    getIntake(focusId, undefined, ctrl.signal).then((d) => { if (!ctrl.signal.aborted) setPinned(d) }).catch(() => { if (!ctrl.signal.aborted) setPinned(null) })
    return () => ctrl.abort()
  }, [follow, focusId, items])

  useEffect(() => {
    setForm(doc ? { ...formFromDoc(doc), forId: doc.id } : emptyReview)
    setTurn(0)
    setZoom(1)
  }, [doc?.id])

  useEffect(() => {
    if (!doc?.has_file) { setSrc(""); setFileType(""); return }
    let url = ""
    const ctrl = new AbortController()
    intakeFile(doc.id, ctrl.signal)
      .then(({ blob, type }) => {
        if (ctrl.signal.aborted) return
        const next = URL.createObjectURL(blob)
        if (ctrl.signal.aborted) { URL.revokeObjectURL(next); return }
        url = next
        setSrc(next)
        setFileType(type)
      })
      .catch(() => { if (!ctrl.signal.aborted) { setSrc(""); setFileType("") } })
    return () => { ctrl.abort(); if (url) URL.revokeObjectURL(url) }
  }, [doc?.id, doc?.has_file])

  useEffect(() => {
    if (!doc || doc.status === "accepted" || doc.status === "rejected") { setLockNote(""); return }
    let stop = false
    lockIntake(doc.id).then(() => { if (!stop) setLockNote("") }).catch((err) => { if (!stop) setLockNote(extractApiError(err).message) })
    return () => { stop = true }
  }, [doc?.id, doc?.status])

  useEffect(() => {
    if (!doc || form.forId !== doc.id || doc.status === "accepted" || doc.status === "rejected") {
      setBills(doc?.status === "accepted" || doc?.status === "rejected" ? (doc.open_bills ?? []) : [])
      return
    }
    const ctrl = new AbortController()
    getIntake(doc.id, { partnerId: form.partnerId, direction: form.direction, choosePartner: true }, ctrl.signal)
      .then((d) => { if (!ctrl.signal.aborted) setBills(d.open_bills ?? []) })
      .catch(() => { if (!ctrl.signal.aborted) setBills([]) })
    return () => ctrl.abort()
  }, [doc, form.forId, form.partnerId, form.direction])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "n" && e.key !== "N") return
      const tag = (e.target as HTMLElement | null)?.tagName
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return
      setFollow(false)
      setIdx((i) => {
        const at = focusId ? items.findIndex((d) => d.id === focusId) : -1
        const base = follow && at >= 0 ? at : i
        return items.length === 0 ? 0 : (base + 1) % items.length
      })
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [items, focusId, follow])

  const closed = doc?.status === "accepted" || doc?.status === "rejected"
  const canConfirm = !!doc && !closed && reviewReady(form, doc.source, !!lockNote)
  const pdf = (doc?.content_type || fileType).includes("pdf")

  const confirm = async () => {
    if (!doc || !canConfirm) return
    const lines = (doc.parsed.lines ?? []).flatMap((ln, i) => {
      if (!ln.product_id) return []
      return [{ product_id: ln.product_id, qty: ln.qty || "1", unit_price: form.prices[String(i)] || "0" }]
    })
    try {
      const res = await acceptIntake(doc.id, {
        direction: form.direction,
        invoice_no: form.invoiceNo,
        invoice_code: form.invoiceCode,
        invoice_type: form.invoiceType,
        partner_id: form.partnerId,
        issue_date: form.issueDate,
        amount_excl_tax: form.excl,
        tax_amount: form.tax,
        total_amount: form.total,
        link_bill_ids: form.links,
        create_draft: form.draft && doc.source !== "receipt" && lines.length > 0,
        match_bill_id: form.matchId || undefined,
        lines,
      }, crypto.randomUUID())
      setNote(res.warning || (res.draft_bill_no ? `已登记发票，采购草稿 ${res.draft_bill_no}` : "已登记发票，库存仍要走审核"))
      onDone?.()
      setIdx(0)
      await load()
    } catch (err) {
      setNote(extractApiError(err).message)
    }
  }

  const reject = async () => {
    if (!doc) return
    const reason = window.prompt("退回原因")
    if (!reason) return
    try {
      await rejectIntake(doc.id, reason)
      setNote("已退回")
      onDone?.()
      setIdx(0)
      await load()
    } catch (err) {
      setNote(extractApiError(err).message)
    }
  }

  return (
    <div className="hidden md:block">
      <p className="mb-3 text-sm text-muted-foreground">
        {waiting ? "正在打开这张收件" : items.length === 0 && !doc ? "没有待核对的收件" : `第 ${Math.min(idx + 1, Math.max(items.length, 1))} / ${Math.max(items.length, doc ? 1 : 0)} 张，按 N 看下一张`}
      </p>
      {lockNote && <p className="mb-3 text-sm">{lockNote}</p>}
      {doc?.status === "accepted" && <p className="mb-3 text-sm">这张票已经入账。{doc.invoice_id && <a className="underline" href={intakeDuplicateHref("invoice", doc.invoice_id, doc.invoice_no)}>打开这张发票</a>}</p>}
      {doc?.status === "rejected" && <p className="mb-3 text-sm">已退回。{doc.reject_reason}</p>}
      {doc && (
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            {src && pdf && <iframe title="收件原件" src={src} className="h-[32rem] w-full rounded border" />}
            {src && !pdf && (
              <div className="max-h-[32rem] overflow-auto rounded border">
                <img src={src} alt="收件原图" className="w-full object-contain" style={{ width: `${zoom * 100}%`, transform: `rotate(${turn}deg)` }} />
              </div>
            )}
            {!src && <p className="text-sm text-muted-foreground">这张没有原图</p>}
            {src && !pdf && (
              <div className="mt-2 flex gap-2">
                <Button type="button" variant="outline" onClick={() => setTurn((t) => (t + 90) % 360)}>旋转</Button>
                <Button type="button" variant="outline" onClick={() => setZoom((z) => z === 1 ? 2 : 1)}>{zoom === 1 ? "放大" : "还原"}</Button>
              </div>
            )}
            {src && <a className="mt-2 inline-block text-sm underline" href={src} target="_blank" rel="noreferrer">新窗口打开原件</a>}
          </div>
          {!closed && <ReviewFields doc={doc} form={form} bills={bills} onChange={(patch) => setForm((f) => ({ ...f, ...patch }))} />}
        </div>
      )}
      <div className="mt-4 flex gap-2">
        <Button type="button" disabled={!canConfirm} onClick={() => { void confirm() }}>确认</Button>
        <Button type="button" variant="outline" disabled={!doc || closed || !!lockNote} onClick={() => { void reject() }}>不是这张</Button>
      </div>
      {note && <p className="mt-3 text-sm">{note}</p>}
    </div>
  )
}
