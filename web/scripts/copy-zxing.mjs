// 把 zxing-wasm 的读码 wasm 拷到 public/zxing,由本站自托管(不走第三方 CDN)。
// 用 bun 执行: bun scripts/copy-zxing.mjs。产物已提交进仓库,此脚本在安装/升级依赖时刷新。
import { copyFileSync, existsSync, mkdirSync, readdirSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = join(dirname(fileURLToPath(import.meta.url)), "..")
const src = join(root, "node_modules", "zxing-wasm", "dist", "reader")
const dst = join(root, "public", "zxing")

if (!existsSync(src)) {
  console.warn("[copy-zxing] 未找到 zxing-wasm,跳过")
  process.exit(0)
}
mkdirSync(dst, { recursive: true })
for (const f of readdirSync(src).filter((n) => n.endsWith(".wasm"))) {
  copyFileSync(join(src, f), join(dst, f))
  console.log(`[copy-zxing] ${f} -> public/zxing/`)
}
