"use client"

import React, { useState, useEffect, useRef, useCallback } from "react"
import { listProducts, type Product } from "@/lib/api/products"

interface ProductSearchProps {
  onSelect: (product: Product) => void
  tenantId?: string
  lastAddedProductId?: string
}

const BARCODE_RE = /^\d+$/

/**
 * ProductSearch provides a debounced input that supports both barcode scanning
 * (numeric-only input → attribute_filter lookup) and name search (free text → q param).
 *
 * Single barcode match → auto-adds to cart without user confirmation.
 * Multiple name results → shows a dropdown (up to 8 items), keyboard navigable.
 */
export const ProductSearch = React.forwardRef<HTMLInputElement, ProductSearchProps>(
  function ProductSearch({ onSelect, tenantId }, ref) {
    const [query, setQuery] = useState("")
    const [results, setResults] = useState<Product[]>([])
    const [highlighted, setHighlighted] = useState(0)
    const [loading, setLoading] = useState(false)
    const [notFound, setNotFound] = useState(false)

    const internalRef = useRef<HTMLInputElement>(null)
    // Merge the forwarded ref with the internal ref so we can use both
    const inputRef = (ref as React.RefObject<HTMLInputElement>) ?? internalRef
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

    const runSearch = useCallback(
      async (q: string) => {
        if (!q.trim()) {
          setResults([])
          setNotFound(false)
          return
        }

        const isBarcode = BARCODE_RE.test(q.trim())
        setLoading(true)

        try {
          const res = isBarcode
            ? await listProducts({
                attributes_filter: { barcode: q.trim() },
                limit: 1,
                tenantId,
              })
            : await listProducts({ q: q.trim(), limit: 20, tenantId })

          const items = res.items ?? []

          if (isBarcode && items.length === 1) {
            // Auto-select on unambiguous barcode scan
            onSelect(items[0])
            setQuery("")
            setResults([])
            setNotFound(false)
          } else if (isBarcode && items.length === 0) {
            setResults([])
            setNotFound(true)
          } else {
            setResults(items.slice(0, 8))
            setNotFound(false)
          }
        } catch {
          setResults([])
        } finally {
          setLoading(false)
        }
      },
      [onSelect, tenantId]
    )

    useEffect(() => {
      if (!query.trim()) {
        setResults([])
        setNotFound(false)
        setLoading(false)
        if (debounceRef.current) clearTimeout(debounceRef.current)
        return
      }

      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => {
        runSearch(query)
      }, 200)

      return () => {
        if (debounceRef.current) clearTimeout(debounceRef.current)
      }
    }, [query, runSearch])

    const handleSelect = useCallback(
      (product: Product) => {
        onSelect(product)
        setQuery("")
        setResults([])
        setHighlighted(0)
        inputRef.current?.focus()
      },
      [onSelect, inputRef]
    )

    const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter") {
        if (results.length > 0) {
          e.preventDefault()
          handleSelect(results[highlighted])
        } else if (query.trim()) {
          // Immediate barcode lookup on Enter without waiting for debounce
          if (debounceRef.current) clearTimeout(debounceRef.current)
          runSearch(query)
        }
      } else if (e.key === "ArrowDown") {
        e.preventDefault()
        setHighlighted((h) => Math.min(h + 1, results.length - 1))
      } else if (e.key === "ArrowUp") {
        e.preventDefault()
        setHighlighted((h) => Math.max(h - 1, 0))
      } else if (e.key === "Escape") {
        setResults([])
        setQuery("")
      }
    }

    return (
      <div className="relative w-full">
        <input
          ref={inputRef}
          type="text"
          role="textbox"
          autoFocus
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setHighlighted(0)
          }}
          onKeyDown={handleKeyDown}
          placeholder="商品名 / 编码 / 扫码 (F1)"
          className="w-full rounded-lg border border-border bg-background px-4 py-3 text-xl outline-none focus:ring-2 focus:ring-ring placeholder:text-muted-foreground"
        />

        {loading && (
          <div className="absolute right-3 top-3 text-sm text-muted-foreground">搜索中...</div>
        )}

        {notFound && (
          <div className="absolute left-0 right-0 top-full z-20 mt-1 rounded-lg border border-border bg-background p-3 text-sm text-muted-foreground shadow-lg">
            未找到条码商品
          </div>
        )}

        {results.length > 0 && (
          <ul
            role="listbox"
            className="absolute left-0 right-0 top-full z-20 mt-1 max-h-72 overflow-y-auto rounded-lg border border-border bg-background shadow-lg"
          >
            {results.map((product, idx) => (
              <li
                key={product.id}
                role="option"
                aria-selected={idx === highlighted}
                onClick={() => handleSelect(product)}
                onMouseEnter={() => setHighlighted(idx)}
                className={`cursor-pointer px-4 py-2.5 text-sm transition-colors ${
                  idx === highlighted ? "bg-muted" : "hover:bg-muted/50"
                }`}
              >
                <span className="font-medium">{product.name}</span>
                {product.code && (
                  <span className="ml-2 text-xs text-muted-foreground font-mono">
                    {product.code}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    )
  }
)
