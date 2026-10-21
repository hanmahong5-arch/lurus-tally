/**
 * Minimal QR Code generator: byte mode, error correction level L, versions
 * 1-10 (up to 271 bytes). Enough for an invite link; no dependency, so the
 * link never leaves the browser.
 */

const ECC_PER_BLOCK = [7, 10, 15, 20, 26, 18, 20, 24, 30, 18]
const NUM_BLOCKS = [1, 1, 1, 1, 1, 2, 2, 2, 2, 4]
const MAX_VERSION = ECC_PER_BLOCK.length

function rawModules(ver: number): number {
  let r = (16 * ver + 128) * ver + 64
  if (ver >= 2) {
    const n = Math.floor(ver / 7) + 2
    r -= (25 * n - 10) * n - 55
    if (ver >= 7) r -= 36
  }
  return r
}

function dataCodewords(ver: number): number {
  return Math.floor(rawModules(ver) / 8) - ECC_PER_BLOCK[ver - 1] * NUM_BLOCKS[ver - 1]
}

function gfMul(x: number, y: number): number {
  let z = 0
  for (let i = 7; i >= 0; i--) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d)
    z ^= ((y >>> i) & 1) * x
  }
  return z
}

function rsDivisor(degree: number): number[] {
  const result = new Array<number>(degree).fill(0)
  result[degree - 1] = 1
  let root = 1
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < degree; j++) {
      result[j] = gfMul(result[j], root)
      if (j + 1 < degree) result[j] ^= result[j + 1]
    }
    root = gfMul(root, 0x02)
  }
  return result
}

function rsRemainder(data: number[], divisor: number[]): number[] {
  const result = new Array<number>(divisor.length).fill(0)
  for (const b of data) {
    const factor = b ^ (result.shift() as number)
    result.push(0)
    divisor.forEach((coef, i) => {
      result[i] ^= gfMul(coef, factor)
    })
  }
  return result
}

function alignmentPositions(ver: number): number[] {
  if (ver === 1) return []
  const n = Math.floor(ver / 7) + 2
  const size = ver * 4 + 17
  const step = Math.ceil((ver * 4 + 4) / (n * 2 - 2)) * 2
  const out = [6]
  for (let pos = size - 7; out.length < n; pos -= step) out.splice(1, 0, pos)
  return out
}

function maskBit(mask: number, x: number, y: number): boolean {
  switch (mask) {
    case 0: return (x + y) % 2 === 0
    case 1: return y % 2 === 0
    case 2: return x % 3 === 0
    case 3: return (x + y) % 3 === 0
    case 4: return (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0
    case 5: return ((x * y) % 2) + ((x * y) % 3) === 0
    case 6: return ((((x * y) % 2) + ((x * y) % 3)) % 2) === 0
    default: return ((((x + y) % 2) + ((x * y) % 3)) % 2) === 0
  }
}

function buildCodewords(bytes: number[], ver: number): number[] {
  const bits: number[] = []
  const put = (val: number, len: number) => {
    for (let i = len - 1; i >= 0; i--) bits.push((val >>> i) & 1)
  }
  put(0b0100, 4)
  put(bytes.length, ver >= 10 ? 16 : 8)
  bytes.forEach((b) => put(b, 8))
  const capBits = dataCodewords(ver) * 8
  put(0, Math.min(4, capBits - bits.length))
  put(0, (8 - (bits.length % 8)) % 8)
  const data: number[] = []
  for (let i = 0; i < bits.length; i += 8) {
    let v = 0
    for (let j = 0; j < 8; j++) v = (v << 1) | bits[i + j]
    data.push(v)
  }
  for (let pad = 0xec; data.length < dataCodewords(ver); pad ^= 0xec ^ 0x11) data.push(pad)

  const nb = NUM_BLOCKS[ver - 1]
  const eccLen = ECC_PER_BLOCK[ver - 1]
  const raw = Math.floor(rawModules(ver) / 8)
  const numShort = nb - (raw % nb)
  const shortLen = Math.floor(raw / nb)
  const divisor = rsDivisor(eccLen)
  const blocks: number[][] = []
  for (let i = 0, k = 0; i < nb; i++) {
    const dat = data.slice(k, k + shortLen - eccLen + (i < numShort ? 0 : 1))
    k += dat.length
    const ecc = rsRemainder(dat, divisor)
    if (i < numShort) dat.push(0)
    blocks.push(dat.concat(ecc))
  }
  const out: number[] = []
  for (let i = 0; i < blocks[0].length; i++) {
    blocks.forEach((blk, j) => {
      if (i !== shortLen - eccLen || j >= numShort) out.push(blk[i])
    })
  }
  return out
}

/** Dark-module matrix for the given bytes; `forceMask` is for tests only. */
export function qrMatrix(input: string, forceMask?: number): boolean[][] {
  const bytes = Array.from(new TextEncoder().encode(input))
  let ver = 1
  while (ver <= MAX_VERSION) {
    const used = 4 + (ver >= 10 ? 16 : 8) + bytes.length * 8
    if (used <= dataCodewords(ver) * 8) break
    ver++
  }
  if (ver > MAX_VERSION) throw new Error("qr: text too long")

  const size = ver * 4 + 17
  const mod: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false))
  const fn: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false))
  const setFn = (x: number, y: number, dark: boolean) => {
    mod[y][x] = dark
    fn[y][x] = true
  }

  for (let i = 0; i < size; i++) {
    setFn(6, i, i % 2 === 0)
    setFn(i, 6, i % 2 === 0)
  }
  for (const [cx, cy] of [[3, 3], [size - 4, 3], [3, size - 4]]) {
    for (let dy = -4; dy <= 4; dy++) {
      for (let dx = -4; dx <= 4; dx++) {
        const d = Math.max(Math.abs(dx), Math.abs(dy))
        const x = cx + dx
        const y = cy + dy
        if (x >= 0 && x < size && y >= 0 && y < size) setFn(x, y, d !== 2 && d !== 4)
      }
    }
  }
  const al = alignmentPositions(ver)
  al.forEach((cx, i) => {
    al.forEach((cy, j) => {
      if ((i === 0 && j === 0) || (i === 0 && j === al.length - 1) || (i === al.length - 1 && j === 0)) return
      for (let dy = -2; dy <= 2; dy++) {
        for (let dx = -2; dx <= 2; dx++) setFn(cx + dx, cy + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1)
      }
    })
  })

  const drawFormat = (mask: number) => {
    const data = (1 << 3) | mask // level L = 01
    let rem = data
    for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537)
    const bits = ((data << 10) | rem) ^ 0x5412
    const bit = (i: number) => ((bits >>> i) & 1) !== 0
    for (let i = 0; i <= 5; i++) setFn(8, i, bit(i))
    setFn(8, 7, bit(6))
    setFn(8, 8, bit(7))
    setFn(7, 8, bit(8))
    for (let i = 9; i < 15; i++) setFn(14 - i, 8, bit(i))
    for (let i = 0; i < 8; i++) setFn(size - 1 - i, 8, bit(i))
    for (let i = 8; i < 15; i++) setFn(8, size - 15 + i, bit(i))
    setFn(8, size - 8, true)
  }
  drawFormat(0)
  if (ver >= 7) {
    let rem = ver
    for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25)
    const bits = (ver << 12) | rem
    for (let i = 0; i < 18; i++) {
      const b = ((bits >>> i) & 1) !== 0
      const a = size - 11 + (i % 3)
      const c = Math.floor(i / 3)
      setFn(a, c, b)
      setFn(c, a, b)
    }
  }

  const codewords = buildCodewords(bytes, ver)
  let k = 0
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5
    for (let vert = 0; vert < size; vert++) {
      for (let j = 0; j < 2; j++) {
        const x = right - j
        const upward = ((right + 1) & 2) === 0
        const y = upward ? size - 1 - vert : vert
        if (!fn[y][x] && k < codewords.length * 8) {
          mod[y][x] = ((codewords[k >>> 3] >>> (7 - (k & 7))) & 1) !== 0
          k++
        }
      }
    }
  }

  const applyMask = (mask: number) => {
    for (let y = 0; y < size; y++) {
      for (let x = 0; x < size; x++) if (!fn[y][x] && maskBit(mask, x, y)) mod[y][x] = !mod[y][x]
    }
  }
  // Any mask decodes; pick the one with the fewest long runs and 2x2 blocks.
  const penalty = (): number => {
    let p = 0
    for (let a = 0; a < size; a++) {
      for (const horizontal of [true, false]) {
        let run = 1
        for (let b = 1; b < size; b++) {
          const cur = horizontal ? mod[a][b] : mod[b][a]
          const prev = horizontal ? mod[a][b - 1] : mod[b - 1][a]
          if (cur === prev) {
            run++
            if (run === 5) p += 3
            else if (run > 5) p++
          } else run = 1
        }
      }
    }
    for (let y = 0; y < size - 1; y++) {
      for (let x = 0; x < size - 1; x++) {
        const c = mod[y][x]
        if (c === mod[y][x + 1] && c === mod[y + 1][x] && c === mod[y + 1][x + 1]) p += 3
      }
    }
    return p
  }

  let best = forceMask ?? 0
  if (forceMask === undefined) {
    let bestP = Infinity
    for (let m = 0; m < 8; m++) {
      applyMask(m)
      const p = penalty()
      applyMask(m)
      if (p < bestP) {
        bestP = p
        best = m
      }
    }
  }
  applyMask(best)
  drawFormat(best)
  return mod
}

/** SVG path data (one unit per module) for the dark modules. */
export function qrPath(matrix: boolean[][]): string {
  let d = ""
  matrix.forEach((row, y) => {
    row.forEach((dark, x) => {
      if (dark) d += `M${x} ${y}h1v1h-1z`
    })
  })
  return d
}
