import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { signIn } from "@/auth"

export default function LoginPage({
  searchParams,
}: {
  searchParams?: { callbackUrl?: string; error?: string }
}) {
  const callbackUrl = searchParams?.callbackUrl ?? "/dashboard"
  const error = searchParams?.error

  return (
    <main className="flex min-h-screen items-center justify-center bg-background">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-1 text-center">
          <p className="text-sm text-muted-foreground">Lurus Tally</p>
          <CardTitle className="text-2xl font-semibold tracking-tight">
            登录
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-center text-sm text-muted-foreground">
            使用 Zitadel 单点登录
          </p>
          {error ? (
            <p className="text-center text-sm text-red-500">
              登录失败：{error}
            </p>
          ) : null}
          <form
            action={async () => {
              "use server"
              await signIn("zitadel", { redirectTo: callbackUrl })
            }}
          >
            <Button className="w-full" type="submit">
              使用 Lurus 账户登录
            </Button>
          </form>
          <p className="text-center text-xs text-muted-foreground">
            首次登录将引导你创建组织并选择业务类型
          </p>
        </CardContent>
      </Card>
    </main>
  )
}
