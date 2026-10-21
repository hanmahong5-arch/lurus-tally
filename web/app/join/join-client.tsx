"use client"

import { useEffect, useState } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { useSession } from "next-auth/react"

import { signOutForSwitch } from "@/components/account/switch-user"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorBanner } from "@/components/ui/error-banner"
import { useConfirm } from "@/hooks/useConfirm"
import { ApiError, extractApiError } from "@/lib/api/errors"
import { fetchMyShopName } from "./my-shop-name"
import { joinShop, type JoinResult } from "@/lib/api/team"
import { normalizeRole, roleLabel } from "@/lib/roles"

/** The link's shop/role parameters are display-only hints; keep them short and plain. */
function hint(value: string | null): string {
  return (value ?? "").trim().slice(0, 40)
}

/** True when the server says the person's own shop is empty and may be given up. */
function canAbandonEmptyShop(err: unknown): boolean {
  if (!(err instanceof ApiError) || err.code !== "already_in_shop") return false
  return (err.body as { can_abandon_empty_shop?: boolean } | undefined)?.can_abandon_empty_shop === true
}

/** True when the server says the invite is one of the person's own shop's. */
function isOwnShopInvite(err: unknown): boolean {
  return err instanceof ApiError && err.code === "already_member_here"
}

const OWN_SHOP_NOTICE = "这是你自己店的邀请链接，请发给对方，用他自己的手机和账号打开"

/** Accepts a shop invite link: /join?token=... */
export function JoinClient() {
  const router = useRouter()
  const params = useSearchParams()
  const { data: session, update } = useSession()
  const confirm = useConfirm()
  const token = params.get("token") ?? ""
  const shopHint = hint(params.get("shop"))
  const roleHint = hint(params.get("role"))
  const inviteId = hint(params.get("i"))
  const user = session?.user
  const hasShop = !!user?.tenantId
  const myRole = hasShop ? normalizeRole(user?.role, user?.isOwner === true) : null
  const [myShop, setMyShop] = useState("")
  const [ownInvite, setOwnInvite] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [joined, setJoined] = useState<JoinResult | null>(null)

  // The shop's name is not in the session; read it from /me so the person can see which account and shop they are in.
  useEffect(() => {
    if (!user?.tenantId) return
    let alive = true
    fetchMyShopName(user.tenantId)
      .then((name) => alive && setMyShop(hint(name)))
      .catch(() => undefined)
    return () => {
      alive = false
    }
  }, [user?.tenantId])

  // A link with an invite id is judged by the server alone; only older links fall back to comparing names.
  const sameShopLink = ownInvite || (!inviteId && hasShop && !!shopHint && shopHint === myShop)
  const who = user?.name || user?.email || ""
  const where = hasShop
    ? `${myShop ? `${myShop}的` : ""}${myRole ? roleLabel(myRole) : ""}`
    : ""

  function handleSwitch() {
    void signOutForSwitch(confirm, myRole, `/join?${params.toString()}`)
  }

  async function finish(res: JoinResult) {
    // Pull the new shop and role into the session before showing the result.
    await update()
    setJoined(res)
  }

  async function handleJoin() {
    setBusy(true)
    setError(null)
    try {
      await finish(await (inviteId ? joinShop(token, { inviteId }) : joinShop(token)))
    } catch (err) {
      if (isOwnShopInvite(err)) {
        setOwnInvite(true)
      } else if (canAbandonEmptyShop(err)) {
        const ok = await confirm({
          title: "放弃你刚开的空店铺，再加入老板的店？",
          body:
            "你的账号之前开过一个店铺，里面只有系统自带的示例数据，没有你录入的商品和单据。" +
            "放弃后这个店铺会停用，不能恢复；之后你就是老板那个店铺的成员，看到的是老板店里的数据。",
          confirmText: "放弃空店并加入",
          cancelText: "先不加入",
          danger: true,
        })
        if (ok) {
          try {
            await finish(await joinShop(token, { abandonEmptyShop: true, ...(inviteId ? { inviteId } : {}) }))
          } catch (err2) {
            if (isOwnShopInvite(err2)) setOwnInvite(true)
            else setError(extractApiError(err2).message)
          }
        } else {
          setError("已保留你原来的店铺；想加入请换一个没开过店的账号，或联系客服。")
        }
      } else {
        setError(extractApiError(err).message)
      }
      setBusy(false)
    }
  }

  if (joined) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-background p-4">
        <Card className="w-full max-w-sm">
          <CardHeader className="space-y-1 text-center">
            <p className="text-sm text-muted-foreground">Lurus Tally</p>
            <CardTitle className="text-2xl font-semibold tracking-tight">加入成功</CardTitle>
            <CardDescription>
              {`你已加入「${joined.shop_name}」，角色：${joined.role_label}`}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button className="w-full" type="button" onClick={() => router.replace("/dashboard")}>
              进入店铺
            </Button>
          </CardContent>
        </Card>
      </main>
    )
  }

  const intro = shopHint
    ? `老板邀请你加入「${shopHint}」${roleHint ? `，角色：${roleHint}` : ""}，点下面的按钮就能加入。`
    : "店主邀请你加入他的店铺，点下面的按钮就能加入。"

  return (
    <main className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-1 text-center">
          <p className="text-sm text-muted-foreground">Lurus Tally</p>
          <CardTitle className="text-2xl font-semibold tracking-tight">加入店铺</CardTitle>
          <CardDescription>
            {token ? intro : "邀请链接不完整，请向店主要一个新的邀请。"}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {who && (
            <p className="text-center text-sm">
              {`当前登录：${who}${where ? `（${where}）` : ""}`}
            </p>
          )}
          {error && <ErrorBanner>{error}</ErrorBanner>}
          {token && sameShopLink && (
            <p className="rounded-md border bg-muted p-3 text-sm text-muted-foreground">{OWN_SHOP_NOTICE}</p>
          )}
          {token && !sameShopLink && (
            <Button className="w-full" type="button" disabled={busy} onClick={handleJoin}>
              {busy ? "正在加入…" : "加入店铺"}
            </Button>
          )}
          {who && (
            <Button className="w-full" type="button" variant="outline" disabled={busy} onClick={handleSwitch}>
              不是你？换个账号
            </Button>
          )}
        </CardContent>
      </Card>
    </main>
  )
}
