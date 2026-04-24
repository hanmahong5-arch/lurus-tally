"use client"

import { useEffect, useState } from "react"
import { getRateOn } from "@/lib/api/currency"

interface RateInputProps {
  /** The selected currency code (from CurrencySelector) */
  currency: string
  /** Current rate value as string (e.g. "7.25") */
  value: string
  /** Called when the rate changes */
  onChange: (rate: string) => void
  /** Date for rate lookup in YYYY-MM-DD format; defaults to today */
  date?: string
  disabled?: boolean
  className?: string
}

/**
 * RateInput displays an exchange rate input field.
 * When currency changes and is non-CNY, it automatically fetches the most
 * recent rate and pre-fills the input. The user can manually override.
 * When currency is CNY, the input is disabled and fixed to "1".
 */
export function RateInput({
  currency,
  value,
  onChange,
  date,
  disabled,
  className,
}: RateInputProps) {
  const [source, setSource] = useState<string>("")
  const [warning, setWarning] = useState<string>("")
  const [fetching, setFetching] = useState(false)

  const isCNY = !currency || currency === "CNY"

  useEffect(() => {
    if (isCNY) {
      onChange("1")
      setSource("")
      setWarning("")
      return
    }

    let cancelled = false
    setFetching(true)
    const dateStr = date ?? new Date().toISOString().slice(0, 10)
    getRateOn(currency, "CNY", dateStr)
      .then((result) => {
        if (!cancelled) {
          onChange(result.rate)
          setSource(result.source)
          setWarning(result.warning ?? "")
          setFetching(false)
        }
      })
      .catch(() => {
        if (!cancelled) {
          setFetching(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [currency, date]) // eslint-disable-line react-hooks/exhaustive-deps

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    // Allow only digits and a single decimal point.
    const v = e.target.value.replace(/[^0-9.]/g, "").replace(/^(\d*\.?\d{0,8}).*$/, "$1")
    onChange(v)
  }

  if (isCNY) {
    return (
      <input
        type="text"
        value="1"
        disabled
        className={className}
        aria-label="汇率（CNY 固定 1）"
      />
    )
  }

  return (
    <div className="space-y-1">
      <input
        type="text"
        value={value}
        onChange={handleChange}
        disabled={disabled || fetching}
        className={className}
        placeholder="汇率（如 7.25）"
        aria-label={`${currency} → CNY 汇率`}
        inputMode="decimal"
      />
      {source && !warning && (
        <p className="text-xs text-muted-foreground">
          来源：{source} · {date ?? new Date().toISOString().slice(0, 10)} 录入
        </p>
      )}
      {warning === "no_rate_found" && (
        <p className="text-xs text-yellow-600">
          未找到历史汇率，请手动填写
        </p>
      )}
    </div>
  )
}
