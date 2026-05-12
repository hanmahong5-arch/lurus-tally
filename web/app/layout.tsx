import type { Metadata, Viewport } from "next"
import { ThemeProvider } from "next-themes"
import { ToastProvider } from "@/components/providers/toast-provider"
import { ConfirmProvider } from "@/hooks/useConfirm"
import "@/app/globals.css"

export const metadata: Metadata = {
  title: "Lurus Tally",
  description: "AI-native 智能进销存",
}

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  maximumScale: 5,
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body>
        <ThemeProvider
          attribute="class"
          defaultTheme="dark"
          enableSystem={false}
          disableTransitionOnChange
        >
          <ToastProvider>
            <ConfirmProvider>{children}</ConfirmProvider>
          </ToastProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
