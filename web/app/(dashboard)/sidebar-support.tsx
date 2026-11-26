import { BookOpenIcon, LifeBuoyIcon } from "lucide-react"

/** Help footer under the account card. External links, quieter than the navigation. */
export function SupportLinks() {
  const linkCls =
    "inline-flex items-center gap-1 rounded-sm transition-colors hover:text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
  return (
    <div className="flex items-center gap-3 px-3 pb-0.5 text-[11px] text-muted-foreground">
      <a href="https://docs.lurus.cn" target="_blank" rel="noreferrer" className={linkCls}>
        <BookOpenIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
        文档
      </a>
      <a href="mailto:support@lurus.cn" className={linkCls}>
        <LifeBuoyIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
        支持
      </a>
    </div>
  )
}
