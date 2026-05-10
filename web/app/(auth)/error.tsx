"use client"

import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default function AuthError({
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
    console.error("[auth-error]", error)
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
          <CardTitle>登录页加载失败</CardTitle>
          <CardDescription>错误码 {errorId}</CardDescription>
        </CardHeader>
        <CardContent className="flex gap-2">
          <Button onClick={() => reset()}>重试</Button>
          <Button variant="outline" onClick={() => router.push("/login")}>
            返回登录
          </Button>
          <Button variant="ghost" onClick={copy}>
            {copied ? "已复制" : "复制错误码"}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
