"use client"

interface HsCodeInputProps {
  value: string
  onChange: (v: string) => void
  disabled?: boolean
  className?: string
}

/** Valid HS code lengths per international standards. */
const VALID_LENGTHS = new Set([6, 8, 10])

/**
 * HsCodeInput is a controlled text input for HS (Harmonized System) codes.
 *
 * Constraints:
 *   - Only accepts numeric characters (0-9)
 *   - Valid lengths are 6, 8, or 10 digits (international HS6 / CN HS10)
 *   - Other lengths show a warning but do NOT prevent form submission
 *
 * Values are stored in `product.attributes.hs_code` (JSONB).
 */
export function HsCodeInput({
  value,
  onChange,
  disabled,
  className,
}: HsCodeInputProps) {
  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    // Allow: backspace, delete, tab, escape, enter, arrow keys, home, end.
    if (
      ["Backspace", "Delete", "Tab", "Escape", "Enter",
        "ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)
    ) {
      return
    }
    // Allow Ctrl+A/C/V/X
    if ((e.ctrlKey || e.metaKey) && ["a", "c", "v", "x"].includes(e.key.toLowerCase())) {
      return
    }
    // Block non-digit keys
    if (!/^\d$/.test(e.key)) {
      e.preventDefault()
    }
  }

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    // Strip any non-digit that slips through (e.g. paste)
    const digits = e.target.value.replace(/\D/g, "")
    onChange(digits)
  }

  const len = value.length
  const isEmpty = len === 0
  const isValid = isEmpty || VALID_LENGTHS.has(len)
  const showWarning = !isEmpty && !isValid

  return (
    <div className="space-y-1">
      <input
        type="text"
        value={value}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        disabled={disabled}
        className={[
          className,
          showWarning ? "border-yellow-500 focus:ring-yellow-400" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        placeholder="输入 HS 编码（6/8/10 位）"
        title="中国海关 10 位，国际 HS 6 位"
        inputMode="numeric"
        maxLength={12}
        aria-label="HS 编码"
        aria-invalid={showWarning}
      />
      {showWarning && (
        <p className="text-xs text-yellow-600" role="alert">
          HS 编码通常为 6、8 或 10 位数字（当前 {len} 位，保存不受影响）
        </p>
      )}
    </div>
  )
}
