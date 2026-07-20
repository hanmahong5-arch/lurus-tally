import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { signIn } from "@/auth"

/**
 * loginErrorMessage maps a NextAuth/OIDC signin error CODE (the raw
 * `?error=` query value) to a user-readable Chinese message. Paying users must
 * never see a machine code like "OAuthCallbackError" or "Configuration"; any
 * unrecognized code falls back to a generic message rather than being echoed.
 *
 * Codes per NextAuth v5 SignInPageErrorParam + common OAuth provider errors.
 */
function loginErrorMessage(code: string): string {
  switch (code) {
    case "Configuration":
      return "登录服务暂时不可用，请稍后再试或联系支持。"
    case "AccessDenied":
      return "你的账户没有访问权限，请联系管理员开通。"
    case "Verification":
      return "登录链接已失效，请重新发起登录。"
    case "OAuthSignin":
    case "OAuthCallbackError":
    case "OAuthCallback":
    case "Callback":
      return "登录过程中断，请重新点击登录。"
    case "OAuthAccountNotLinked":
      return "该邮箱已用其他方式注册，请使用原有方式登录。"
    case "SessionRequired":
      return "请先登录后再继续。"
    default:
      return "登录失败，请重新尝试；若反复失败请联系支持。"
  }
}

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
            使用单点登录 (SSO)
          </p>
          {error ? (
            <p className="text-center text-sm text-red-500">
              {loginErrorMessage(error)}
            </p>
          ) : null}
          <form
            action={async () => {
              "use server"
              await signIn("oidc", { redirectTo: callbackUrl })
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
