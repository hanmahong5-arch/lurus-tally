import path from "path"
import { fileURLToPath } from "url"

const __dirname = path.dirname(fileURLToPath(import.meta.url))

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
  // Ensure standalone output places server.js at the project root (required for Docker)
  experimental: {
    outputFileTracingRoot: __dirname,
  },
  // Account-center migration (Phase 1) — fold legacy entry routes into the
  // unified /account?tab=... tab nav. Non-permanent so we can iterate without
  // baking the redirect into client caches.
  async redirects() {
    return [
      { source: "/settings/api-keys", destination: "/account?tab=api-keys", permanent: false },
      { source: "/subscription", destination: "/account?tab=subscription", permanent: false },
    ]
  },
}

export default nextConfig
