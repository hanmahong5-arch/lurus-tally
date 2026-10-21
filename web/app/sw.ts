/// <reference lib="webworker" />
import { CacheFirst, ExpirationPlugin, NetworkFirst, NetworkOnly, Serwist, type PrecacheEntry, type SerwistGlobalConfig } from "serwist"

declare global {
  interface WorkerGlobalScope extends SerwistGlobalConfig {
    __SW_MANIFEST: (PrecacheEntry | string)[] | undefined
  }
}

declare const self: ServiceWorkerGlobalScope

/**
 * 离线壳策略：
 *  - 预缓存 Next 构建产物（__SW_MANIFEST）与 /offline 兜底页；
 *  - /api/** 一律只走网络，绝不缓存（接口数据与登录态不能落进缓存）；
 *  - /_next/static/** 缓存优先（文件名带哈希，内容不可变）；
 *  - 仅 /pos 与 /pos/history 的导航网络优先，3 秒没回应就用上次缓存的页面；退出登录时清空 pages 缓存；
 *    其余页面不缓存，离线时落到 /offline。
 *    /pos、/pos/history 需要登录，不能预缓存，靠收银员在线时打开过一次来缓存。
 */
const serwist = new Serwist({
  precacheEntries: self.__SW_MANIFEST,
  skipWaiting: true,
  clientsClaim: true,
  navigationPreload: false,
  runtimeCaching: [
    { matcher: ({ url }) => url.pathname.startsWith("/api/"), handler: new NetworkOnly() },
    {
      matcher: ({ url }) => url.pathname.startsWith("/_next/static/"),
      handler: new CacheFirst({
        cacheName: "next-static",
        plugins: [new ExpirationPlugin({ maxEntries: 200, maxAgeSeconds: 30 * 24 * 3600 })],
      }),
    },
    {
      // 只缓存收银相关页面：其它页面的 HTML 内嵌登录人会话，不能留给下一个登录人。
      matcher: ({ request, url }) =>
        request.mode === "navigate" && (url.pathname === "/pos" || url.pathname === "/pos/history"),
      handler: new NetworkFirst({
        cacheName: "pages",
        networkTimeoutSeconds: 3,
        plugins: [new ExpirationPlugin({ maxEntries: 30 })],
      }),
    },
  ],
  fallbacks: {
    entries: [{ url: "/offline", matcher: ({ request }) => request.destination === "document" }],
  },
})

serwist.addEventListeners()
