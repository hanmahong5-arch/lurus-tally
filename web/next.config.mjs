import path from "path"
import { fileURLToPath } from "url"
import { readdirSync, readFileSync } from "node:fs"

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// UAT-3 Bug 3 gate: NEXT_PUBLIC_DEV_TENANT_ID gets inlined into the client
// bundle, so if it leaks into a production build every visitor inherits the
// dev tenant context — bypassing real auth. Fail the build hard.
// Set TALLY_ALLOW_DEV_TENANT=true to opt out (staging only — do not use in prod).
if (
  process.env.NODE_ENV === "production" &&
  process.env.NEXT_PUBLIC_DEV_TENANT_ID &&
  process.env.NEXT_PUBLIC_DEV_TENANT_ID.trim() !== "" &&
  process.env.TALLY_ALLOW_DEV_TENANT !== "true"
) {
  throw new Error(
    "NEXT_PUBLIC_DEV_TENANT_ID must be empty in production builds. " +
      "It would be baked into the client bundle and bypass tenant auth. " +
      "Unset it before `bun run build`, or set TALLY_ALLOW_DEV_TENANT=true for staging."
  )
}

// Anti-regression source scan. The env check above only catches the *value* at
// build time; this fails any build (regardless of NODE_ENV) if the literal
// NEXT_PUBLIC_DEV_TENANT_ID is reintroduced anywhere under the app source. The
// dev-tenant scaffold was removed in favour of the session-derived useTenantId()
// hook — reviving the env var would re-open the same client-bundle tenant-bypass.
// auth.ts (project root, not scanned) legitimately injects the dev tenant into
// the session server-side; that is the single sanctioned path.
const SCAN_DIRS = ["app", "components", "lib", "hooks"]
const CODE_EXT = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"])
const FORBIDDEN_TOKEN = "NEXT_PUBLIC_DEV_TENANT_ID"

function scanForForbiddenToken(dir) {
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch {
    return // dir absent in some build contexts — nothing to scan
  }
  for (const entry of entries) {
    if (entry.name === "node_modules" || entry.name === ".next") continue
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      scanForForbiddenToken(full)
    } else if (CODE_EXT.has(path.extname(entry.name))) {
      if (readFileSync(full, "utf8").includes(FORBIDDEN_TOKEN)) {
        throw new Error(
          `${FORBIDDEN_TOKEN} found in ${full}. The dev-tenant scaffold was removed; ` +
            "derive the tenant from the session via useTenantId() instead. A literal " +
            `${FORBIDDEN_TOKEN} is inlined into the client bundle and bypasses tenant auth.`
        )
      }
    }
  }
}

for (const d of SCAN_DIRS) {
  scanForForbiddenToken(path.join(__dirname, d))
}

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
  // Ensure standalone output places server.js at the project root (required for Docker)
  outputFileTracingRoot: __dirname,
  experimental: {},
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
