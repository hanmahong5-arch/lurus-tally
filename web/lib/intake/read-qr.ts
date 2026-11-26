import { isTaxQR } from "./tax-qr"

/** Reads one tax QR from an image file. A PDF or a failed decode returns "". */
export async function readTaxQR(file: Blob): Promise<string> {
  if (!file.type.startsWith("image/") || typeof createImageBitmap !== "function") return ""
  const bmp = await createImageBitmap(file).catch(() => null)
  if (!bmp) return ""
  const canvas = document.createElement("canvas")
  canvas.width = bmp.width
  canvas.height = bmp.height
  const ctx = canvas.getContext("2d")
  if (!ctx) return ""
  ctx.drawImage(bmp, 0, 0)
  bmp.close?.()
  const mod = await import("zxing-wasm/reader")
  await mod.prepareZXingModule({ overrides: { locateFile: (p: string) => `/zxing/${p}` }, fireImmediately: true })
  const found = await mod.readBarcodes(ctx.getImageData(0, 0, canvas.width, canvas.height), {
    formats: ["QRCode"],
    maxNumberOfSymbols: 1,
  })
  const text = found[0]?.text ?? ""
  return isTaxQR(text) ? text : ""
}
