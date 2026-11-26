/** A tax QR is the national comma form or a verification URL. Product barcodes are not. */
export function isTaxQR(text: string): boolean {
  const t = text.trim()
  return t.startsWith("01,") || t.includes("://")
}
