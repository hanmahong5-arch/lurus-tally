import Link from "next/link"

/** Service Worker 的离线兜底页：没缓存过的页面离线时落到这里。静态页，不读会话。 */
export default function OfflinePage() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 px-4 text-center">
      <h1 className="text-xl font-semibold">当前离线，可进入收银</h1>
      <p className="text-sm text-muted-foreground">网络恢复前，收银单会先记在本机，联网后自动上传。</p>
      <Link href="/pos" className="rounded-md bg-primary px-6 py-2 text-sm font-medium text-primary-foreground">
        进入收银台
      </Link>
    </main>
  )
}
