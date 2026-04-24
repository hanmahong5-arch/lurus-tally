"use client"

import { useEffect, useState } from "react"
import { getCurrencies, type Currency } from "@/lib/api/currency"

interface CurrencySelectorProps {
  /** Currently selected currency code */
  value: string
  /** Called with the new currency code when selection changes */
  onChange: (code: string) => void
  disabled?: boolean
  className?: string
}

/**
 * CurrencySelector fetches available currencies from the API and renders a
 * select dropdown. CNY is always displayed first.
 */
export function CurrencySelector({
  value,
  onChange,
  disabled,
  className,
}: CurrencySelectorProps) {
  const [currencies, setCurrencies] = useState<Currency[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getCurrencies()
      .then((data) => {
        if (!cancelled) {
          // Sort: CNY first, then alphabetically
          const sorted = [...data].sort((a, b) => {
            if (a.code === "CNY") return -1
            if (b.code === "CNY") return 1
            return a.code.localeCompare(b.code)
          })
          setCurrencies(sorted)
          setLoading(false)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(String(err))
          setLoading(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return (
      <select className={className} disabled value={value}>
        <option value={value}>{value || "CNY"}</option>
      </select>
    )
  }

  return (
    <select
      className={className}
      value={value || "CNY"}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled || loading}
      aria-label="货币选择"
    >
      {loading && <option value="">加载中...</option>}
      {currencies.map((c) => (
        <option key={c.code} value={c.code}>
          {c.code} — {c.name}
        </option>
      ))}
    </select>
  )
}
