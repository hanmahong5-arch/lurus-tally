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
}

export default nextConfig
