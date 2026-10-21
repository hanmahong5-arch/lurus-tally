// Generates the PWA icons into public/icons/. Run: bun run scripts/gen-icons.ts
// Pure TS PNG encoder (zlib from the runtime), no extra dependencies.
import { deflateSync } from "node:zlib"
import { mkdirSync, writeFileSync } from "node:fs"
import path from "node:path"

const ACCENT: [number, number, number] = [0xff, 0x5d, 0x1f] // brand orange, same as --lt-accent
const DARK: [number, number, number] = [0x0a, 0x0a, 0x0a]
const WHITE: [number, number, number] = [0xff, 0xff, 0xff]

const crcTable = new Uint32Array(256).map((_, n) => {
  let c = n
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  return c >>> 0
})
function crc32(buf: Uint8Array): number {
  let c = 0xffffffff
  for (let i = 0; i < buf.length; i++) c = crcTable[(c ^ buf[i]) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}
function chunk(type: string, data: Uint8Array): Buffer {
  const out = Buffer.alloc(12 + data.length)
  out.writeUInt32BE(data.length, 0)
  out.write(type, 4, "ascii")
  out.set(data, 8)
  out.writeUInt32BE(crc32(out.subarray(4, 8 + data.length)), 8 + data.length)
  return out
}

export function encodePng(size: number, rgb: Uint8Array): Buffer {
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(size, 0)
  ihdr.writeUInt32BE(size, 4)
  ihdr[8] = 8 // bit depth
  ihdr[9] = 6 // RGBA
  const stride = size * 4
  const raw = Buffer.alloc((stride + 1) * size)
  for (let y = 0; y < size; y++) {
    raw[y * (stride + 1)] = 0
    raw.set(rgb.subarray(y * stride, (y + 1) * stride), y * (stride + 1) + 1)
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", new Uint8Array(0)),
  ])
}

function inRoundRect(x: number, y: number, x0: number, y0: number, x1: number, y1: number, r: number): boolean {
  if (x < x0 || x >= x1 || y < y0 || y >= y1) return false
  const cx = x < x0 + r ? x0 + r : x >= x1 - r ? x1 - r : x
  const cy = y < y0 + r ? y0 + r : y >= y1 - r ? y1 - r : y
  return (x - cx) ** 2 + (y - cy) ** 2 <= r * r
}

// maskable: the orange fills the whole canvas and the glyph stays inside the central safe zone.
// any: dark canvas with a rounded orange tile.
export function render(size: number, maskable: boolean): Uint8Array {
  const px = new Uint8Array(size * size * 4)
  const tile = size * 0.1
  const gw = size * (maskable ? 0.5 : 0.56)
  const g0 = (size - gw) / 2
  const bar = gw * 0.22
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      let c = DARK
      let a = 0 // any-purpose icons keep transparent corners outside the rounded tile
      if (maskable || inRoundRect(x, y, tile, tile, size - tile, size - tile, size * 0.2)) {
        c = ACCENT
        a = 255
      }
      // glyph "T": top bar + centered stem
      const top = x >= g0 && x < g0 + gw && y >= g0 && y < g0 + bar
      const stem = x >= size / 2 - bar / 2 && x < size / 2 + bar / 2 && y >= g0 && y < g0 + gw
      if (top || stem) {
        c = WHITE
        a = 255
      }
      const i = (y * size + x) * 4
      px[i] = c[0]
      px[i + 1] = c[1]
      px[i + 2] = c[2]
      px[i + 3] = a
    }
  }
  return px
}

if (process.argv[1]?.endsWith("gen-icons.ts")) {
  const outDir = path.join(path.dirname(process.argv[1]), "..", "public", "icons")
  mkdirSync(outDir, { recursive: true })
  const targets: [string, number, boolean][] = [
    ["icon-192.png", 192, false],
    ["icon-512.png", 512, false],
    ["icon-maskable-512.png", 512, true],
    ["apple-touch-icon.png", 180, true],
  ]
  for (const [name, size, maskable] of targets) {
    writeFileSync(path.join(outDir, name), encodePng(size, render(size, maskable)))
    console.log("wrote", name)
  }
}
