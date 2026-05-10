import type { Metadata } from "next"
import { ThemeProvider } from "next-themes"
import { ToastProvider } from "@/components/providers/toast-provider"
import "@/app/globals.css"

export const metadata: Metadata = {
  title: "Lurus Tally",
  description: "AI-native 智能进销存",
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
          <ToastProvider>{children}</ToastProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
