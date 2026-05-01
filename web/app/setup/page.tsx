import { redirect } from "next/navigation"
import { auth } from "@/auth"
import { chooseProfile, getMe, TallyApiError, type ProfileType } from "@/lib/api/me"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Button } from "@/components/ui/button"

// Setup is the first-login onboarding screen. The user picks their business
// type (cross-border vs retail) which determines the inventory method, default
// modules, and dashboard layout.
//
// Server-side guards:
//   - not authenticated → /login (handled by middleware before reaching here)
//   - already has a profile → straight to /dashboard
export default async function SetupPage({
  searchParams,
}: {
  searchParams?: { error?: string }
}) {
  const session = await auth()
  if (!session?.accessToken) {
    redirect("/login")
  }

  // Fast-path: if profile already chosen, skip the form. This handles refreshes
  // and direct URL access after onboarding completes.
  try {
    const me = await getMe(session.accessToken)
    if (me.profile_type) {
      redirect("/dashboard")
    }
  } catch {
    // Backend unreachable — surface the form anyway; user can retry submit.
  }

  const error = searchParams?.error

  async function submitProfile(formData: FormData) {
    "use server"
    const profileType = formData.get("profile_type") as ProfileType | null
    if (profileType !== "cross_border" && profileType !== "retail" && profileType !== "horticulture") {
      redirect("/setup?error=invalid")
    }
    const s = await auth()
    if (!s?.accessToken) {
      redirect("/login")
    }
    try {
      await chooseProfile(s.accessToken, profileType!)
    } catch (e) {
      if (e instanceof TallyApiError) {
        redirect(`/setup?error=${encodeURIComponent(e.detail ?? "api_error")}`)
      }
      redirect("/setup?error=network")
    }
    // Successful choice — go straight to dashboard. The next request will
    // re-fetch /me via the jwt callback (TTL is 60s; a fresh fetch happens
    // because isFirstTime was true on the previous fetch).
    redirect("/dashboard")
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-4 py-12">
      <div className="w-full max-w-3xl space-y-8">
        <div className="text-center space-y-2">
          <p className="text-sm text-muted-foreground">Lurus Tally</p>
          <h1 className="text-3xl font-bold tracking-tight">欢迎，先告诉我们你的业务类型</h1>
          <p className="text-sm text-muted-foreground">
            这一步决定库存计价方式、默认模块和数据看板布局。之后可在设置里调整。
          </p>
        </div>

        {error ? (
          <div className="rounded-md bg-destructive/10 p-3 text-center text-sm text-destructive">
            操作失败：{error}
          </div>
        ) : null}

        <form action={submitProfile} className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <ProfileOption
            value="retail"
            title="零售 / 批发"
            description="开店、批发、电商。库存按移动加权平均（WAC）计价；适合 SKU 流转快、利润看汇总的业务。"
            highlights={["💵 即时收银（POS）", "📦 单一库存视图", "📊 销售毛利日报"]}
          />
          <ProfileOption
            value="cross_border"
            title="跨境贸易"
            description="进出口、跨境电商。库存按 FIFO 批次计价；自动多币种、汇率、HS Code 支持。"
            highlights={["🌐 多币种 + 汇率", "🚢 报关 / HS Code", "📈 批次成本可追溯"]}
          />
          <ProfileOption
            value="horticulture"
            title="苗木 / 园林工程"
            description="苗圃、园林公司、工程方。苗木字典、项目制核算、价格分级，内置 200+ 常用苗木品种。"
            highlights={["🌿 苗木字典 + 价格历史", "🏗️ 项目制损益核算", "📸 现场拍照存档"]}
          />
        </form>

        <p className="text-center text-xs text-muted-foreground">
          点击任一卡片下方的&ldquo;选这个&rdquo;按钮即可完成。混合模式（hybrid）由管理员后台分配。
        </p>
      </div>
    </main>
  )
}

function ProfileOption({
  value,
  title,
  description,
  highlights,
}: {
  value: ProfileType
  title: string
  description: string
  highlights: string[]
}) {
  return (
    <Card className="flex flex-col">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col justify-between gap-4">
        <ul className="space-y-2 text-sm text-muted-foreground">
          {highlights.map((h) => (
            <li key={h}>{h}</li>
          ))}
        </ul>
        <Button type="submit" name="profile_type" value={value} className="w-full">
          选这个
        </Button>
      </CardContent>
    </Card>
  )
}
