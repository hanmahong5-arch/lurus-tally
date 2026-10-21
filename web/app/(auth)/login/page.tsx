import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { cookies } from "next/headers"
import { devProviderEnabled, signIn } from "@/auth"
import { SWITCH_COOKIE } from "@/lib/device-keys"
import { WeChatOpenHint } from "@/components/wechat-open-hint"

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
  searchParams?: { callbackUrl?: string; error?: string; switch?: string }
}) {
  const callbackUrl = searchParams?.callbackUrl ?? "/dashboard"
  const error = searchParams?.error
  // Signed out to hand the till to someone else: make the identity provider ask for the account again.
  // The cookie covers a redirect that dropped the query (bookmark, failed callback retry).
  const switching = searchParams?.switch === "1" || cookies().get(SWITCH_COOKIE)?.value === "1"
  const authParams = switching ? { prompt: "login" } : undefined
  // An invite link sends the person here first; the page should say what happens next.
  const joiningShop = callbackUrl === "/join" || callbackUrl.startsWith("/join?")
  // 微信登录入口：平台侧就绪前默认不显示。靠 IdP 提示参数 idp_hint 让身份提供方直接走微信，
  // 仍然是同一个 oidc provider，不影响现有登录。
  const wechatLogin = process.env.NEXT_PUBLIC_WECHAT_LOGIN === "true"
  const wechatHint = process.env.NEXT_PUBLIC_WECHAT_IDP_HINT || "wechat"
  const wechatAuthParams = { ...(authParams ?? {}), idp_hint: wechatHint }

  return (
    <main className="flex min-h-screen flex-col bg-background">
      <WeChatOpenHint />
      <div className="flex flex-1 items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-1 text-center">
          <p className="text-sm text-muted-foreground">Lurus Tally</p>
          <CardTitle className="text-2xl font-semibold tracking-tight">
            登录
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-center text-sm text-muted-foreground">
            {joiningShop ? "登录后加入老板的店铺" : switching ? "已退出。下一位请用自己的账号登录" : "登录后进入你的店铺"}
          </p>
          {error ? (
            <p className="text-center text-sm text-red-500">
              {loginErrorMessage(error)}
            </p>
          ) : null}
          <form
            action={async () => {
              "use server"
              await signIn("oidc", { redirectTo: callbackUrl }, authParams)
            }}
          >
            <Button className="w-full" type="submit">
              使用 Lurus 账户登录
            </Button>
          </form>
          {wechatLogin ? (
            <form
              action={async () => {
                "use server"
                await signIn("oidc", { redirectTo: callbackUrl }, wechatAuthParams)
              }}
            >
              <Button className="w-full" type="submit" variant="outline">
                微信登录
              </Button>
            </form>
          ) : null}
          <p className="text-center text-xs text-muted-foreground">
            {joiningShop
              ? "请用没开过店的账号登录；只开了空店的账号可以在加入时放弃空店"
              : "首次登录将引导你开通自己的店铺并选择业务类型；如果是老板邀请你，请先打开老板发的邀请链接"}
          </p>
          {/* Offline dev login. Rendered only when the dev Credentials provider
              is registered (AUTH_DEV_PROVIDER=true and not production), so a
              local stack without an OIDC client can still sign in. */}
          {devProviderEnabled() ? (
            <form
              className="space-y-3 border-t pt-4"
              action={async (formData: FormData) => {
                "use server"
                await signIn("credentials", {
                  email: formData.get("email"),
                  tenantId: formData.get("tenantId"),
                  redirectTo: callbackUrl,
                })
              }}
            >
              <p className="text-center text-xs text-muted-foreground">本地开发登录（离线，仅开发环境可见）</p>
              <div className="space-y-1">
                <Label htmlFor="dev-email">邮箱</Label>
                <Input id="dev-email" name="email" type="email" placeholder="dev@tally.test" />
              </div>
              <div className="space-y-1">
                <Label htmlFor="dev-tenant">租户 ID（留空自动使用你已有的租户）</Label>
                <Input id="dev-tenant" name="tenantId" placeholder="00000000-0000-0000-0000-000000000001" />
              </div>
              <Button className="w-full" type="submit" variant="outline">
                开发登录
              </Button>
            </form>
          ) : null}
        </CardContent>
      </Card>
      </div>
    </main>
  )
}
