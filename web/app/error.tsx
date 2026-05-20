"use client"

import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default function RootError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  const router = useRouter()
  const [copied, setCopied] = useState(false)
  const errorId = error.digest ?? Math.random().toString(36).slice(2, 10)

  useEffect(() => {
    console.error("[app-error]", error)
  }, [error])

  const copy = async () => {
    await navigator.clipboard.writeText(errorId)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>页面加载失败</CardTitle>
          <CardDescription>错误码 {errorId}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex gap-2">
            <Button onClick={() => reset()}>重试</Button>
            <Button variant="outline" onClick={() => router.push("/dashboard")}>
              返回首页
            </Button>
            <Button variant="ghost" onClick={copy}>
              {copied ? "已复制" : "复制错误码"}
            </Button>
          </div>
          {/* Dev-only: stack trace for easier debugging. Never shown in production. */}
          {process.env.NODE_ENV === "development" && error.message && (
            <pre className="mt-2 max-h-48 overflow-auto rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
              {error.message}
              {error.stack ? "\n" + error.stack : ""}
            </pre>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
