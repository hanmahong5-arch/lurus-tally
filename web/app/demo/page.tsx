import { signIn } from "@/auth"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

// BACKEND_URL is the in-cluster service URL for tally-backend (same default the
// proxy route uses). The demo-start call runs server-side so the public endpoint
// is reached directly, not through the session-gated /api/proxy.
const BACKEND_URL = process.env.BACKEND_URL ?? "http://tally-backend:18200"

// startDemo provisions a fresh, write-isolated sandbox tenant (seeded with real
// nursery data) and signs the visitor in with the throwaway PAT it returns — no
// OIDC, no registration. The "demo" provider turns {tenant_id, token} into a
// horticulture session whose accessToken is the PAT, so /api/proxy forwards it
// and the dashboard works. signIn redirects to /dashboard on success.
async function startDemo() {
  "use server"
  const res = await fetch(`${BACKEND_URL}/api/v1/demo/start`, {
    method: "POST",
    cache: "no-store",
  })
  if (!res.ok) {
    // 404 here means the deployment did not enable TALLY_DEMO_MODE.
    throw new Error(`demo start failed: ${res.status}`)
  }
  const data = (await res.json()) as { tenant_id: string; token: string }
  await signIn("demo", {
    tenantId: data.tenant_id,
    accessToken: data.token,
    redirectTo: "/dashboard",
  })
}

/**
 * DemoPage is the PUBLIC no-OIDC sandbox entry. It is reachable without a session
 * (exempted in middleware) so a prospect — e.g. a nursery owner in a sales
 * interview — can enter a seeded, isolated sandbox in one click and record a real
 * purchase, see stock and AI replenishment, without registering.
 */
export default function DemoPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-background">
      <Card className="w-full max-w-md">
        <CardHeader className="space-y-1 text-center">
          <p className="text-sm text-muted-foreground">Lurus Tally · 苗木进销存</p>
          <CardTitle className="text-2xl font-semibold tracking-tight">免登录体验</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-center text-sm text-muted-foreground">
            进入一个预置真实苗木数据的沙盒：可录进货单、看库存与 AI 补货建议。
            数据相互隔离、随用随弃，无需注册或登录。
          </p>
          <form action={startDemo}>
            <Button className="w-full" type="submit">
              进入苗木演示
            </Button>
          </form>
          <p className="text-center text-xs text-muted-foreground">
            演示数据为示例，仅供体验功能
          </p>
        </CardContent>
      </Card>
    </main>
  )
}
