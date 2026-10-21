import type { Metadata, Viewport } from "next"
import { ThemeProvider } from "next-themes"
import { SessionProvider } from "next-auth/react"
import { auth } from "@/auth"
import { PwaRegister } from "@/components/pwa-register"
import { ErrorReporter } from "@/components/error-reporter"
import { ToastProvider } from "@/components/providers/toast-provider"
import { ConfirmProvider } from "@/hooks/useConfirm"
import "@/app/globals.css"

export const metadata: Metadata = {
  title: "Lurus Tally",
  description: "进销存，开单记账查库存",
  manifest: "/manifest.webmanifest",
  appleWebApp: { capable: true, title: "进销存", statusBarStyle: "default" },
  icons: { apple: "/icons/apple-touch-icon.png" },
}

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  maximumScale: 5,
  themeColor: "#0a0a0a",
}

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  // The session is read on the server and handed down, so useRole() knows the role on the very first render.
  // The backend token stays on the server: server components call auth() themselves and /api/proxy attaches it.
  const session = await auth()
  if (session) delete session.accessToken
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body>
        <ErrorReporter />
        <PwaRegister />
        <ThemeProvider
          attribute="class"
          defaultTheme="dark"
          enableSystem={false}
          disableTransitionOnChange
        >
          {/* SessionProvider exposes the NextAuth session to client hooks
              (useTenantId, useRole). Without it useSession() is unusable app-wide. */}
          <SessionProvider session={session}>
            <ToastProvider>
              <ConfirmProvider>{children}</ConfirmProvider>
            </ToastProvider>
          </SessionProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
