import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default function NotFound() {
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>页面不存在</CardTitle>
          <CardDescription>你访问的地址未找到对应资源</CardDescription>
        </CardHeader>
        <CardContent>
          <Link href="/dashboard">
            <Button>返回首页</Button>
          </Link>
        </CardContent>
      </Card>
    </div>
  )
}
