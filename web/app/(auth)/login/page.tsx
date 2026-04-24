import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"

/**
 * Login page placeholder.
 * Actual Zitadel OIDC integration is deferred to Epic 2 (Story 2.1).
 * This page validates the build chain, OKLCH theme, and shadcn/ui wiring.
 */
export default function LoginPage() {
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
            认证集成将在 Epic 2 完成
          </p>
          {/* Placeholder button — no action wired, OIDC flow is Story 2.1 */}
          <Button className="w-full" disabled>
            使用 Lurus 账户登录
          </Button>
        </CardContent>
      </Card>
    </main>
  )
}
