"use client"

import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default function DashboardError({
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
    console.error("[dashboard-error]", error)
  }, [error])

  const copy = async () => {
    await navigator.clipboard.writeText(errorId)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="flex flex-1 items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>当前模块出错</CardTitle>
          <CardDescription>错误码 {errorId}</CardDescription>
        </CardHeader>
        <CardContent className="flex gap-2">
          <Button onClick={() => reset()}>重试</Button>
          <Button variant="outline" onClick={() => router.push("/dashboard")}>
            返回首页
          </Button>
          <Button variant="ghost" onClick={copy}>
            {copied ? "已复制" : "复制错误码"}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
