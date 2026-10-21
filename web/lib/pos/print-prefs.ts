const KEY = "tally-pos-auto-print"

/** Whether this device prints the receipt by itself right after a sale. */
export function getAutoPrintReceipt(): boolean {
  try {
    return window.localStorage.getItem(KEY) === "1"
  } catch {
    return false
  }
}

export function setAutoPrintReceipt(on: boolean): void {
  try {
    if (on) window.localStorage.setItem(KEY, "1")
    else window.localStorage.removeItem(KEY)
  } catch {
    // storage unavailable (private mode): the choice just isn't remembered
  }
}

const PAPER_KEY = "tally-print-paper-pos"
export type ReceiptPaper = "58" | "80"

/** The till receipt paper width chosen on this device (80 mm unless set otherwise). */
export function getReceiptPaper(): ReceiptPaper {
  try {
    return window.localStorage.getItem(PAPER_KEY) === "58" ? "58" : "80"
  } catch {
    return "80"
  }
}

export function setReceiptPaper(p: ReceiptPaper): void {
  try {
    window.localStorage.setItem(PAPER_KEY, p)
  } catch {
    // storage unavailable (private mode): the choice just isn't remembered
  }
}
