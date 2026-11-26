"use client"

import { useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { isWeChatBrowser } from "@/lib/wechat-env"

/** Records one clip into the same inbox. The words never become a price or a posted bill. */
export function VoiceCapture({ onAudio }: { onAudio: (file: File) => void }) {
  const rec = useRef<MediaRecorder | null>(null)
  const [on, setOn] = useState(false)
  const [note, setNote] = useState("")
  if (isWeChatBrowser()) return null

  const start = async () => {
    setNote("")
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      const chunks: Blob[] = []
      const media = new MediaRecorder(stream)
      rec.current = media
      media.ondataavailable = (e) => { if (e.data.size > 0) chunks.push(e.data) }
      media.onstop = () => {
        stream.getTracks().forEach((t) => t.stop())
        const type = media.mimeType || "audio/webm"
        onAudio(new File([new Blob(chunks, { type })], "voice.webm", { type }))
      }
      media.start()
      setOn(true)
    } catch {
      setNote("麦克风没有打开，可以改传一段录音文件")
    }
  }

  return (
    <span className="inline-flex items-center gap-2">
      {on ? (
        <Button type="button" variant="outline" onClick={() => { rec.current?.stop(); setOn(false) }}>送出这段语音</Button>
      ) : (
        <Button type="button" variant="outline" onClick={() => { void start() }}>开始录音</Button>
      )}
      {note && <span className="text-sm">{note}</span>}
    </span>
  )
}
